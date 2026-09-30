package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"runtime"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"vocatogether/backend/internal/email"
)

const (
	verificationTokenTTL = 24 * time.Hour
	// emailSendTimeout bounds each background send, which outlives its request.
	emailSendTimeout = 10 * time.Second
	// maxInFlightEmails bounds concurrent background sends (see
	// sendInBackground): at least 3 emails a second get out even if every
	// send takes the full timeout.
	maxInFlightEmails = 32
	// hashQueueTimeout bounds the wait for an argon2 slot. A request that
	// waits longer gets ErrOverloaded (503) instead of queueing until its
	// client gives up.
	hashQueueTimeout = 5 * time.Second
)

// Service implements the authentication use cases. It knows nothing about
// HTTP: handlers decode requests and map its errors to responses.
type Service struct {
	pool    *pgxpool.Pool
	sender  email.Sender
	baseURL *url.URL // public base URL for emailed links
	logger  *slog.Logger
	limits  AccountLimits

	sends sync.WaitGroup
	// emailSlots bounds concurrent background sends (see sendInBackground).
	emailSlots chan struct{}

	// hashSlots bounds concurrent argon2 work, shared by every flow that
	// hashes or verifies a password (see withHashSlot).
	hashSlots        chan struct{}
	hashQueueTimeout time.Duration
	// dummyHash is verified against when login finds no usable hash, so
	// unknown emails cost as much as wrong passwords.
	dummyHash string

	// cleanupBatchSize is how many rows each cleanup statement deletes.
	cleanupBatchSize int
}

// NewService returns the auth service. limits are the per-account rate
// limits (NewAccountLimits); the zero AccountLimits disables them.
func NewService(pool *pgxpool.Pool, sender email.Sender, baseURL *url.URL, logger *slog.Logger, limits AccountLimits) *Service {
	return &Service{
		pool:       pool,
		sender:     sender,
		baseURL:    baseURL,
		logger:     logger,
		limits:     limits,
		emailSlots: make(chan struct{}, maxInFlightEmails),
		// argon2 with parallelism 1 is single-threaded CPU work: more
		// concurrent hashes than CPUs add memory (19 MiB each) but no throughput.
		hashSlots:        make(chan struct{}, runtime.GOMAXPROCS(0)),
		hashQueueTimeout: hashQueueTimeout,
		dummyHash:        HashPassword(rand.Text()),
		cleanupBatchSize: cleanupBatchSize,
	}
}

// Register creates an unverified account and emails a verification link.
// If the address already has an account, it changes nothing and emails the
// owner instead. Both outcomes return nil, so callers (and response timing)
// can't tell them apart; only invalid input returns a *ValidationError.
func (s *Service) Register(ctx context.Context, emailInput, password string) error {
	addr, emailErr := NormalizeEmail(emailInput)
	if err := validationError(emailErr, ValidatePassword(password, addr)); err != nil {
		return err
	}
	if err := allowAccount(s.limits.Mail, addr); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("auth: register: %w", err)
	}

	// Hash even when the address turns out to be taken, so the argon2 cost
	// doesn't reveal which addresses have accounts.
	passwordHash, err := s.hashPassword(ctx, password)
	if err != nil {
		return fmt.Errorf("auth: register: %w", err)
	}
	token := NewToken("")

	created, err := createUserWithVerificationToken(ctx, s.pool, addr, passwordHash, token.Hash, verificationTokenTTL)
	if err != nil {
		return fmt.Errorf("auth: register: %w", err)
	}
	if created {
		s.sendInBackground(ctx, "email_verification", verificationEmail(s.baseURL, addr, token.Raw, verificationTokenTTL))
	} else {
		s.sendInBackground(ctx, "account_exists", accountExistsEmail(addr))
	}
	return nil
}

// VerifyEmail consumes a verification token and marks its owner's email as
// verified. It does not log the user in. Every unusable token (malformed,
// unknown, expired, used, or for another purpose) gets the same
// *ValidationError, so the result reveals nothing about accounts or token
// history.
func (s *Service) VerifyEmail(ctx context.Context, rawToken string) error {
	if !wellFormedToken(rawToken, "") {
		return validationError(ErrTokenInvalid)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("auth: verify email: %w", err)
	}

	ok, err := consumeVerificationToken(ctx, s.pool, HashToken(rawToken))
	if err != nil {
		return fmt.Errorf("auth: verify email: %w", err)
	}
	if !ok {
		return validationError(ErrTokenInvalid)
	}
	return nil
}

// WellFormedVerificationToken reports whether raw has the shape of an email
// verification token. It only checks the public format, never the database,
// so the verification page can refuse to render a form for junk without
// revealing anything.
func WellFormedVerificationToken(raw string) bool {
	return wellFormedToken(raw, "")
}

// ResendVerification emails a new verification link if the address belongs
// to an unverified account, which invalidates the previous link. For unknown
// or already verified addresses it does nothing. All three return nil, so the
// result doesn't reveal which addresses have accounts.
func (s *Service) ResendVerification(ctx context.Context, emailInput string) error {
	addr, err := NormalizeEmail(emailInput)
	if err != nil {
		return validationError(err)
	}
	if err := allowAccount(s.limits.Mail, addr); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("auth: resend verification: %w", err)
	}

	token := NewToken("")
	issued, err := reissueVerificationToken(ctx, s.pool, addr, token.Hash, verificationTokenTTL)
	if err != nil {
		return fmt.Errorf("auth: resend verification: %w", err)
	}
	if issued {
		s.sendInBackground(ctx, "email_verification", verificationEmail(s.baseURL, addr, token.Raw, verificationTokenTTL))
	}
	return nil
}

// withHashSlot runs f, which does argon2 work, once a hash slot is free.
// Without this bound, concurrent unauthenticated requests (register, login)
// could allocate 19 MiB each without limit and exhaust memory. Waiting
// requests give up when their context ends, e.g. when the client disconnects,
// or with ErrOverloaded after hashQueueTimeout. Every account state waits in
// the same queue, so queueing time and ErrOverloaded reveal nothing about
// accounts.
func (s *Service) withHashSlot(ctx context.Context, f func()) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	timeout := time.NewTimer(s.hashQueueTimeout)
	defer timeout.Stop()
	select {
	case s.hashSlots <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	case <-timeout.C:
		return ErrOverloaded
	}
	defer func() { <-s.hashSlots }()
	f()
	return nil
}

// hashPassword is HashPassword under the hash limiter.
func (s *Service) hashPassword(ctx context.Context, password string) (hash string, err error) {
	err = s.withHashSlot(ctx, func() { hash = HashPassword(password) })
	return hash, err
}

// verifyPassword is VerifyPassword under the hash limiter. A malformed stored
// hash would fail before any argon2 work and so answer faster; it is verified
// against the dummy hash instead, keeping the cost uniform, and still reported
// as ErrMalformedHash.
func (s *Service) verifyPassword(ctx context.Context, encoded, password string) (ok, needsRehash bool, err error) {
	slotErr := s.withHashSlot(ctx, func() {
		ok, needsRehash, err = VerifyPassword(encoded, password)
		if errors.Is(err, ErrMalformedHash) {
			_, _, _ = VerifyPassword(s.dummyHash, password)
		}
	})
	if slotErr != nil {
		return false, false, slotErr
	}
	return ok, needsRehash, err
}

// sendInBackground delivers msg without delaying the response, so response
// timing doesn't depend on the email provider or reveal which email was sent.
// The send gets its own timeout, detached from the request's cancellation
// (the request ends before the send does) but keeping its values.
//
// Delivery is best effort until a durable outbox exists (decision 018): a 202
// means the request was accepted, never that an email went out. A failed send
// is logged and not retried, and when maxInFlightEmails sends are already
// running the message is dropped and logged, so a slow provider under abuse
// can't pile up goroutines. Either way the user can ask for a new email, and
// the response is the same as for a delivered one. kind names the email type;
// the message itself (recipient, links) and its body are never logged.
//
// An outbox would replace this function's body (insert the message in the
// request's transaction, deliver from a worker); callers need not change.
func (s *Service) sendInBackground(reqCtx context.Context, kind string, msg email.Message) {
	select {
	case s.emailSlots <- struct{}{}:
	default:
		s.logger.WarnContext(reqCtx, "auth: email dropped, too many sends in flight", "kind", kind)
		return
	}
	s.sends.Add(1)
	go func() {
		defer s.sends.Done()
		defer func() { <-s.emailSlots }()
		ctx, cancel := context.WithTimeout(context.WithoutCancel(reqCtx), emailSendTimeout)
		defer cancel()
		if err := s.sender.Send(ctx, msg); err != nil {
			s.logger.ErrorContext(ctx, "auth: send email failed", "kind", kind, "err", err)
		}
	}()
}

// Wait blocks until all background email sends have finished. Each send is
// bounded by emailSendTimeout, so Wait is too once no new requests arrive.
func (s *Service) Wait() {
	s.sends.Wait()
}
