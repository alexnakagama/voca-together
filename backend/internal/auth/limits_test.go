package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"vocatogether/backend/internal/email"
	"vocatogether/backend/internal/ratelimit"
)

// withAccountLimits turns on the production per-account limits (newTestService
// leaves them off so unrelated tests never hit them).
func withAccountLimits(s testService) testService {
	s.limits = NewAccountLimits(slog.New(slog.DiscardHandler))
	return s
}

// dbFreeService has no pool, sender or base URL: any database access or
// email panics, so a call that returns proves it touched neither.
func dbFreeService(t *testing.T) (*Service, *bytes.Buffer) {
	t.Helper()
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	return NewService(nil, nil, nil, logger, NewAccountLimits(logger)), logs
}

func drain(l *ratelimit.Limiter[[32]byte], addr string, n int) {
	for range n {
		l.Allow(sha256.Sum256([]byte(addr)))
	}
}

func requireRateLimited(t *testing.T, err error, around time.Duration) {
	t.Helper()
	var rl *RateLimitedError
	if !errors.As(err, &rl) {
		t.Fatalf("err = %v, want *RateLimitedError", err)
	}
	if rl.RetryAfter <= 0 || rl.RetryAfter > around || rl.RetryAfter < around-time.Minute {
		t.Errorf("RetryAfter = %v, want just under %v", rl.RetryAfter, around)
	}
}

// ---- Per-account limits: enumeration safety ----

// The mail limit is keyed by the normalized address whether or not an account
// exists, so unknown, unverified and verified addresses are limited at the
// same attempt, with the same RetryAfter, and a 429 reveals nothing.
func TestMailLimitIsUniformAcrossAccountStates(t *testing.T) {
	rec := &email.Recorder{}
	s := withAccountLimits(newTestService(t, rec))
	seedUser(t, s.pool, "unverified@example.com")
	seedAccount(t, s.pool, "verified@example.com", "hash", true)

	for _, addr := range []string{"unknown@example.com", "unverified@example.com", "verified@example.com"} {
		for _, op := range []struct {
			name string
			call func() error
		}{
			{"forgot", func() error { return s.ForgotPassword(context.Background(), addr) }},
			{"resend", func() error { return s.ResendVerification(context.Background(), addr) }},
		} {
			s.limits = NewAccountLimits(slog.New(slog.DiscardHandler))
			for i := range mailBurst {
				if err := op.call(); err != nil {
					t.Fatalf("%s %s attempt %d: %v", op.name, addr, i+1, err)
				}
			}
			requireRateLimited(t, op.call(), mailEvery)
		}
	}
}

func TestLoginLimitIsUniformAcrossAccountStates(t *testing.T) {
	s := withAccountLimits(newTestService(t, &email.Recorder{}))
	seedAccount(t, s.pool, "unverified@example.com", HashPassword(testPassword), false)
	seedAccount(t, s.pool, "verified@example.com", HashPassword(testPassword), true)

	for _, tc := range []struct{ addr, password string }{
		{"unknown@example.com", testPassword},
		{"unverified@example.com", testPassword},     // 403 each time
		{"verified@example.com", "wrong-password-1"}, // 401 each time
		{"verified@example.com", testPassword},       // success counts too
	} {
		s.limits = NewAccountLimits(slog.New(slog.DiscardHandler))
		for i := range loginBurst {
			_, err := s.Login(context.Background(), tc.addr, tc.password, "")
			var rl *RateLimitedError
			if errors.As(err, &rl) {
				t.Fatalf("%s: limited at attempt %d", tc.addr, i+1)
			}
		}
		_, err := s.Login(context.Background(), tc.addr, tc.password, "")
		requireRateLimited(t, err, loginEvery)
	}
}

// A limited request does no database or argon2 work and sends nothing,
// whatever the address (the service has no pool: any access would panic).
func TestLimitedRequestsTouchNothing(t *testing.T) {
	s, _ := dbFreeService(t)
	saturate := func() { // any argon2 attempt now blocks forever
		s.hashSlots = make(chan struct{}, 1)
		s.hashSlots <- struct{}{}
	}
	saturate()

	drain(s.limits.Login, "ana@example.com", loginBurst)
	_, err := s.Login(context.Background(), "  Ana@Example.COM ", testPassword, "")
	requireRateLimited(t, err, loginEvery)

	drain(s.limits.Mail, "bob@example.com", mailBurst)
	requireRateLimited(t, s.Register(context.Background(), "bob@example.com", testPassword), mailEvery)
	requireRateLimited(t, s.ResendVerification(context.Background(), "BOB@example.com"), mailEvery)
	requireRateLimited(t, s.ForgotPassword(context.Background(), " bob@example.com"), mailEvery)
}

// Validation still comes first: a 422 is never replaced by a 429, and invalid
// input consumes nothing.
func TestValidationPrecedesAccountLimits(t *testing.T) {
	s, _ := dbFreeService(t)
	drain(s.limits.Mail, "bob@example.com", mailBurst)
	drain(s.limits.Login, "bob@example.com", loginBurst)

	var verr *ValidationError
	if err := s.Register(context.Background(), "bob@example.com", "short"); !errors.As(err, &verr) {
		t.Errorf("register: err = %v, want *ValidationError", err)
	}
	if _, err := s.Login(context.Background(), "bob@example.com", "", ""); !errors.As(err, &verr) {
		t.Errorf("login: err = %v, want *ValidationError", err)
	}
	if err := s.ForgotPassword(context.Background(), "not-an-email"); !errors.As(err, &verr) {
		t.Errorf("forgot: err = %v, want *ValidationError", err)
	}
}

// register, resend and forgot draw from one mail bucket per address: the
// mailbox is what's protected, whichever endpoint mails it.
func TestMailLimitIsSharedAcrossEndpoints(t *testing.T) {
	rec := &email.Recorder{}
	s := withAccountLimits(newTestService(t, rec))
	ctx := context.Background()

	calls := []func() error{
		func() error { return s.Register(ctx, "ana@example.com", testPassword) },
		func() error { return s.ResendVerification(ctx, "ana@example.com") },
		func() error { return s.ForgotPassword(ctx, "ana@example.com") },
		func() error { return s.Register(ctx, "ana@example.com", testPassword) },
		func() error { return s.ForgotPassword(ctx, "ana@example.com") },
	}
	for i, call := range calls {
		if err := call(); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}
	requireRateLimited(t, s.ResendVerification(ctx, "ana@example.com"), mailEvery)
	s.Wait()
	if n := len(rec.Messages()); n != mailBurst {
		t.Errorf("sent %d emails, want %d", n, mailBurst)
	}
	// Another address is unaffected.
	if err := s.ForgotPassword(ctx, "other@example.com"); err != nil {
		t.Errorf("other address: %v", err)
	}
}

func TestLimitedRequestsLogNoSecrets(t *testing.T) {
	s, logs := dbFreeService(t)
	drain(s.limits.Login, "ana@example.com", loginBurst)
	drain(s.limits.Mail, "ana@example.com", mailBurst)
	for range 3 {
		_, _ = s.Login(context.Background(), "ana@example.com", testPassword, "")
		_ = s.ForgotPassword(context.Background(), "ana@example.com")
	}
	for _, secret := range []string{"ana@example.com", testPassword} {
		if strings.Contains(logs.String(), secret) {
			t.Errorf("log contains %q:\n%s", secret, logs)
		}
	}
}

// ---- Abuse under concurrency ----

func TestConcurrentLoginsForOneAddressReachArgon2AtMostBurstTimes(t *testing.T) {
	s := withAccountLimits(newTestService(t, &email.Recorder{}))
	var mu sync.Mutex
	var limited, rejected int
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			_, err := s.Login(context.Background(), "nobody@example.com", testPassword, "")
			var rl *RateLimitedError
			mu.Lock()
			defer mu.Unlock()
			switch {
			case errors.As(err, &rl):
				limited++
			case errors.Is(err, ErrInvalidCredentials):
				rejected++
			default:
				t.Errorf("unexpected err: %v", err)
			}
		})
	}
	wg.Wait()
	if rejected != loginBurst || limited != 50-loginBurst {
		t.Fatalf("invalid_credentials = %d, limited = %d; want %d and %d", rejected, limited, loginBurst, 50-loginBurst)
	}
}

func TestConcurrentForgotForOneAddressSendsAtMostBurstEmails(t *testing.T) {
	rec := &email.Recorder{}
	s := withAccountLimits(newTestService(t, rec))
	seedAccount(t, s.pool, "ana@example.com", "hash", true)

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { _ = s.ForgotPassword(context.Background(), "ana@example.com") })
	}
	wg.Wait()
	s.Wait()
	if n := len(rec.Messages()); n != mailBurst {
		t.Fatalf("sent %d reset emails, want %d", n, mailBurst)
	}
}

// ---- argon2 queue bound ----

func TestHashQueueWaitIsBounded(t *testing.T) {
	s, _ := dbFreeService(t)
	s.hashQueueTimeout = 30 * time.Millisecond
	s.hashSlots = make(chan struct{}, 1)
	s.hashSlots <- struct{}{}

	ran := false
	start := time.Now()
	err := s.withHashSlot(context.Background(), func() { ran = true })
	if !errors.Is(err, ErrOverloaded) || ran {
		t.Fatalf("err = %v, ran = %v; want ErrOverloaded without running", err, ran)
	}
	if waited := time.Since(start); waited < 30*time.Millisecond || waited > time.Second {
		t.Errorf("waited %v, want about the queue timeout", waited)
	}

	// Register hashes before touching the database, so it fails the same way
	// (the service has no pool) and writes nothing.
	if err := s.Register(context.Background(), "ana@example.com", testPassword); !errors.Is(err, ErrOverloaded) {
		t.Errorf("register: err = %v, want ErrOverloaded", err)
	}
}

func TestLoginOverloadedIsUniform(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	seedAccount(t, s.pool, "ana@example.com", HashPassword(testPassword), true)
	s.hashQueueTimeout = 20 * time.Millisecond
	saturateHashSlots(s)
	for _, addr := range []string{"ana@example.com", "nobody@example.com"} {
		if _, err := s.Login(context.Background(), addr, testPassword, ""); !errors.Is(err, ErrOverloaded) {
			t.Errorf("%s: err = %v, want ErrOverloaded", addr, err)
		}
	}
	requireNoSessions(t, s.pool)
}

func TestNewServiceUsesDefaultQueueTimeout(t *testing.T) {
	s, _ := dbFreeService(t)
	if s.hashQueueTimeout != hashQueueTimeout {
		t.Errorf("hashQueueTimeout = %v, want %v", s.hashQueueTimeout, hashQueueTimeout)
	}
	if cap(s.emailSlots) != maxInFlightEmails {
		t.Errorf("email slots = %d, want %d", cap(s.emailSlots), maxInFlightEmails)
	}
}

// ---- Bounded background email ----

func TestBackgroundSendsAreBoundedAndExcessIsDropped(t *testing.T) {
	logs := &bytes.Buffer{}
	release := make(chan struct{})
	var mu sync.Mutex
	var started, delivered int
	sender := senderFunc(func(ctx context.Context, msg email.Message) error {
		mu.Lock()
		started++
		mu.Unlock()
		<-release
		mu.Lock()
		delivered++
		mu.Unlock()
		return nil
	})
	s := NewService(nil, sender, nil, slog.New(slog.NewTextHandler(logs, nil)), AccountLimits{})
	msg := email.Message{To: "ana@example.com", Subject: "s", Text: "t"}

	for range maxInFlightEmails + 3 {
		s.sendInBackground(context.Background(), "password_reset", msg)
	}
	close(release)
	s.Wait()

	if started != maxInFlightEmails || delivered != maxInFlightEmails {
		t.Errorf("started = %d, delivered = %d; want %d", started, delivered, maxInFlightEmails)
	}
	out := logs.String()
	if n := strings.Count(out, "auth: email dropped"); n != 3 {
		t.Errorf("dropped logs = %d, want 3:\n%s", n, out)
	}
	if !strings.Contains(out, "kind=password_reset") || strings.Contains(out, "ana@example.com") {
		t.Errorf("drop log must name the kind only:\n%s", out)
	}

	// Slots are released: sending works again.
	s.sendInBackground(context.Background(), "password_reset", msg)
	s.Wait()
	if delivered != maxInFlightEmails+1 {
		t.Errorf("send after release: delivered = %d", delivered)
	}
}

// A dropped email doesn't change the result: the caller still gets success,
// exactly as when the email goes out (202 never promises delivery).
func TestDroppedEmailKeepsResultUnchanged(t *testing.T) {
	rec := &email.Recorder{}
	s := newTestService(t, rec)
	seedAccount(t, s.pool, "ana@example.com", "hash", true)
	for range maxInFlightEmails {
		s.emailSlots <- struct{}{}
	}
	if err := s.ForgotPassword(context.Background(), "ana@example.com"); err != nil {
		t.Fatal(err)
	}
	for range maxInFlightEmails {
		<-s.emailSlots
	}
	s.Wait()
	if n := len(rec.Messages()); n != 0 {
		t.Errorf("sent %d emails, want 0 (dropped)", n)
	}
	if !strings.Contains(s.logs.String(), "auth: email dropped") {
		t.Errorf("drop not logged:\n%s", s.logs)
	}
}
