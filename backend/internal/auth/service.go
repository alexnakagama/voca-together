package auth

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"vocatogether/backend/internal/email"
)

const (
	verificationTokenTTL = 24 * time.Hour
	// emailSendTimeout bounds each background send, which outlives its request.
	emailSendTimeout = 10 * time.Second
)

// Service implements the authentication use cases. It knows nothing about
// HTTP: handlers decode requests and map its errors to responses.
type Service struct {
	pool    *pgxpool.Pool
	sender  email.Sender
	baseURL *url.URL // public base URL for emailed links
	logger  *slog.Logger
	sends   sync.WaitGroup
}

func NewService(pool *pgxpool.Pool, sender email.Sender, baseURL *url.URL, logger *slog.Logger) *Service {
	return &Service{pool: pool, sender: sender, baseURL: baseURL, logger: logger}
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
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("auth: register: %w", err)
	}

	// Hash even when the address turns out to be taken, so the argon2 cost
	// doesn't reveal which addresses have accounts.
	passwordHash := HashPassword(password)
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

// sendInBackground delivers msg without delaying the response, so response
// timing doesn't depend on the email provider or reveal which email was sent.
// The send gets its own timeout, detached from the request's cancellation
// (the request ends before the send does) but keeping its values.
//
// A failed send is logged and not retried: the user can ask for a new email.
// kind names the email type; the message itself (recipient, links) and its
// body are never logged.
func (s *Service) sendInBackground(reqCtx context.Context, kind string, msg email.Message) {
	s.sends.Add(1)
	go func() {
		defer s.sends.Done()
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
