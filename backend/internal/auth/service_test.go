package auth

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"vocatogether/backend/internal/email"
	"vocatogether/backend/internal/testutil"
)

const (
	testPassword = "plum-lantern-47-orbit"
	testBaseURL  = "https://api.example.com"
)

// senderFunc adapts a function to email.Sender for tests that need to
// observe the send itself.
type senderFunc func(ctx context.Context, msg email.Message) error

func (f senderFunc) Send(ctx context.Context, msg email.Message) error { return f(ctx, msg) }

type testService struct {
	*Service
	pool *pgxpool.Pool
	logs *bytes.Buffer // read only after Wait
}

func newTestService(t *testing.T, sender email.Sender) testService {
	t.Helper()
	pool := testutil.DB(t)
	logs := &bytes.Buffer{}
	svc := NewService(pool, sender, mustParseURL(t, testBaseURL), slog.New(slog.NewTextHandler(logs, nil)), AccountLimits{}, nil)
	t.Cleanup(svc.Wait)
	return testService{Service: svc, pool: pool, logs: logs}
}

// dumpTables returns every row of every auth table as text, to check that no
// secret appears anywhere in them.
func dumpTables(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT u::text FROM users u UNION ALL SELECT t::text FROM user_tokens t
		 UNION ALL SELECT s::text FROM sessions s UNION ALL SELECT i::text FROM user_identities i`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var sb strings.Builder
	for rows.Next() {
		var row string
		if err := rows.Scan(&row); err != nil {
			t.Fatal(err)
		}
		sb.WriteString(row)
		sb.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return sb.String()
}

func TestRegisterCreatesUserAndSendsVerificationEmail(t *testing.T) {
	rec := &email.Recorder{}
	s := newTestService(t, rec)

	if err := s.Register(context.Background(), "ana@example.com", testPassword); err != nil {
		t.Fatal(err)
	}
	s.Wait()

	var passwordHash string
	var tokenHash []byte
	var verified bool
	err := s.pool.QueryRow(context.Background(),
		`SELECT u.password_hash, t.token_hash, u.email_verified_at IS NOT NULL
		 FROM users u JOIN user_tokens t ON t.user_id = u.id AND t.purpose = 'email_verification'
		 WHERE u.email = 'ana@example.com'`).Scan(&passwordHash, &tokenHash, &verified)
	if err != nil {
		t.Fatal(err)
	}
	if verified {
		t.Error("new user is already verified")
	}
	if ok, _, err := VerifyPassword(passwordHash, testPassword); err != nil || !ok {
		t.Errorf("stored hash does not verify the password (ok=%v, err=%v)", ok, err)
	}

	msgs := rec.Messages()
	if len(msgs) != 1 {
		t.Fatalf("sent %d emails, want 1", len(msgs))
	}
	rawToken := linkIn(t, msgs[0].Text).Query().Get("token")
	if want := verificationEmail(mustParseURL(t, testBaseURL), "ana@example.com", rawToken, 24*time.Hour); msgs[0] != want {
		t.Errorf("email = %#v, want the 24-hour verification email", msgs[0])
	}
	if !bytes.Equal(tokenHash, HashToken(rawToken)) {
		t.Error("stored token_hash is not the SHA-256 of the emailed token")
	}

	// Neither secret is stored in any form other than its hash.
	dump := dumpTables(t, s.pool)
	for name, secret := range map[string]string{"password": testPassword, "token": rawToken} {
		if strings.Contains(dump, secret) {
			t.Errorf("plaintext %s stored in the database", name)
		}
	}
}

func TestRegisterNormalizesEmail(t *testing.T) {
	rec := &email.Recorder{}
	s := newTestService(t, rec)

	if err := s.Register(context.Background(), "  Ana@Example.COM ", testPassword); err != nil {
		t.Fatal(err)
	}
	s.Wait()

	if n := countRows(t, s.pool, `SELECT count(*) FROM users WHERE email = 'ana@example.com'`); n != 1 {
		t.Errorf("normalized user rows = %d, want 1", n)
	}
	if msgs := rec.Messages(); len(msgs) != 1 || msgs[0].To != "ana@example.com" {
		t.Errorf("email not sent to the normalized address")
	}
}

func TestRegisterValidation(t *testing.T) {
	tests := []struct {
		name, email, password string
		want                  []*FieldError
	}{
		{"invalid email", "not-an-email", testPassword, []*FieldError{ErrEmailInvalid}},
		{"empty email", "", testPassword, []*FieldError{ErrEmailInvalid}},
		{"password too short", "ana@example.com", "abc123x", []*FieldError{ErrPasswordTooShort}},
		{"password too long", "ana@example.com", strings.Repeat("x", PasswordMaxLength+1), []*FieldError{ErrPasswordTooLong}},
		{"password too common", "ana@example.com", "password123", []*FieldError{ErrPasswordTooCommon}},
		// The comparison uses the normalized email.
		{"password same as email", "  Ana@Example.com ", "ANA@example.com", []*FieldError{ErrPasswordSameAsEmail}},
		{"both invalid", "nope", "abc123x", []*FieldError{ErrEmailInvalid, ErrPasswordTooShort}},
	}
	rec := &email.Recorder{}
	s := newTestService(t, rec)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := s.Register(context.Background(), tt.email, tt.password)
			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("err = %v, want *ValidationError", err)
			}
			if !slices.Equal(verr.Fields, tt.want) {
				t.Errorf("fields = %v, want %v", verr.Fields, tt.want)
			}
			if strings.Contains(err.Error(), tt.password) {
				t.Errorf("error leaks the password: %v", err)
			}
		})
	}
	s.Wait()
	if n := countRows(t, s.pool, `SELECT count(*) FROM users`); n != 0 {
		t.Errorf("users = %d, want 0", n)
	}
	if n := len(rec.Messages()); n != 0 {
		t.Errorf("sent %d emails, want 0", n)
	}
}

func TestRegisterExistingEmail(t *testing.T) {
	for _, second := range []string{"ana@example.com", " ANA@Example.com"} {
		t.Run(second, func(t *testing.T) {
			rec := &email.Recorder{}
			s := newTestService(t, rec)
			ctx := context.Background()
			if err := s.Register(ctx, "ana@example.com", testPassword); err != nil {
				t.Fatal(err)
			}
			before := dumpTables(t, s.pool)

			if err := s.Register(ctx, second, "a-different-password-9"); err != nil {
				t.Fatalf("existing email: err = %v, want nil (same result as a new account)", err)
			}
			s.Wait()

			// Password not overwritten, no new or replaced token.
			if after := dumpTables(t, s.pool); after != before {
				t.Errorf("registering an existing email changed the database:\nbefore %s\nafter  %s", before, after)
			}
			msgs := rec.Messages()
			if len(msgs) != 2 {
				t.Fatalf("sent %d emails, want 2", len(msgs))
			}
			if msgs[1] != accountExistsEmail("ana@example.com") {
				t.Errorf("second email = %#v, want the account-exists email", msgs[1])
			}
		})
	}
}

func TestRegisterSendsOnlyAfterCommit(t *testing.T) {
	var s testService
	var committed bool
	s = newTestService(t, senderFunc(func(ctx context.Context, msg email.Message) error {
		// A different connection sees the user only once the transaction committed.
		var n int
		err := s.pool.QueryRow(ctx, `SELECT count(*) FROM user_tokens`).Scan(&n)
		committed = err == nil && n == 1
		return err
	}))

	if err := s.Register(context.Background(), "ana@example.com", testPassword); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	if !committed {
		t.Error("email sent before user and token were committed")
	}
}

// The request's context ends as soon as the response is written; the email
// must still be sent, with its own deadline.
func TestRegisterEmailOutlivesRequestContextWithDeadline(t *testing.T) {
	rec := &email.Recorder{} // rejects cancelled contexts
	var remaining time.Duration
	var hasDeadline bool
	s := newTestService(t, senderFunc(func(ctx context.Context, msg email.Message) error {
		var deadline time.Time
		deadline, hasDeadline = ctx.Deadline()
		remaining = time.Until(deadline)
		return rec.Send(ctx, msg)
	}))

	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Register(ctx, "ana@example.com", testPassword); err != nil {
		t.Fatal(err)
	}
	cancel()
	s.Wait()

	if n := len(rec.Messages()); n != 1 {
		t.Errorf("sent %d emails after the request ended, want 1", n)
	}
	if !hasDeadline || remaining <= 0 || remaining > emailSendTimeout {
		t.Errorf("send had %v left (deadline set: %v), want a deadline within %v", remaining, hasDeadline, emailSendTimeout)
	}
}

func TestRegisterEmailFailureKeepsAccountAndLeaksNothing(t *testing.T) {
	var sent email.Message
	s := newTestService(t, senderFunc(func(ctx context.Context, msg email.Message) error {
		sent = msg
		return errors.New("provider unavailable")
	}))

	if err := s.Register(context.Background(), "ana@example.com", testPassword); err != nil {
		t.Fatalf("err = %v, want nil: email failure must not change the result", err)
	}
	s.Wait()

	if n := countRows(t, s.pool, `SELECT count(*) FROM user_tokens`); n != 1 {
		t.Errorf("tokens = %d, want 1 (account stays, user can request a new email)", n)
	}
	logs := s.logs.String()
	if !strings.Contains(logs, "provider unavailable") || !strings.Contains(logs, "kind=email_verification") {
		t.Errorf("failure not logged: %s", logs)
	}
	rawToken := linkIn(t, sent.Text).Query().Get("token")
	for name, secret := range map[string]string{"password": testPassword, "token": rawToken, "email": "ana@example.com"} {
		if strings.Contains(logs, secret) {
			t.Errorf("logs contain the %s: %s", name, logs)
		}
	}
}

func TestRegisterHonorsCancelledContext(t *testing.T) {
	rec := &email.Recorder{}
	s := newTestService(t, rec)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := s.Register(ctx, "ana@example.com", testPassword); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	s.Wait()
	if n := countRows(t, s.pool, `SELECT count(*) FROM users`); n != 0 {
		t.Errorf("users = %d, want 0", n)
	}
	if n := len(rec.Messages()); n != 0 {
		t.Errorf("sent %d emails, want 0", n)
	}
}

func TestRegisterDatabaseFailure(t *testing.T) {
	rec := &email.Recorder{}
	s := newTestService(t, rec)
	s.pool.Close()

	err := s.Register(context.Background(), "ana@example.com", testPassword)
	var verr *ValidationError
	if err == nil || errors.As(err, &verr) {
		t.Fatalf("err = %v, want a database error", err)
	}
	for _, secret := range []string{"ana@example.com", testPassword} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error leaks %q: %v", secret, err)
		}
	}
	s.Wait()
	if n := len(rec.Messages()); n != 0 {
		t.Errorf("sent %d emails after a failed registration, want 0", n)
	}
}

// ---- Password hashing limiter ----

func TestHashSlotsBoundConcurrency(t *testing.T) {
	s := &Service{hashSlots: make(chan struct{}, 2), hashQueueTimeout: hashQueueTimeout}
	var mu sync.Mutex
	running, peak := 0, 0
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			err := s.withHashSlot(context.Background(), func() {
				mu.Lock()
				running++
				peak = max(peak, running)
				mu.Unlock()
				time.Sleep(5 * time.Millisecond)
				mu.Lock()
				running--
				mu.Unlock()
			})
			if err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if peak != 2 {
		t.Errorf("peak concurrency = %d, want 2", peak)
	}
}

func TestHashSlotWaitHonorsContext(t *testing.T) {
	s := &Service{hashSlots: make(chan struct{}, 1), hashQueueTimeout: hashQueueTimeout}
	s.hashSlots <- struct{}{} // the only slot is busy
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	ran := false
	err := s.withHashSlot(ctx, func() { ran = true })
	if !errors.Is(err, context.DeadlineExceeded) || ran {
		t.Fatalf("err = %v, ran = %v; want DeadlineExceeded without running", err, ran)
	}
}

func TestNewServiceSizesHashSlotsToCPUs(t *testing.T) {
	s := NewService(nil, nil, nil, slog.New(slog.DiscardHandler), AccountLimits{}, nil)
	if got, want := cap(s.hashSlots), runtime.GOMAXPROCS(0); got != want {
		t.Errorf("hash slots = %d, want GOMAXPROCS = %d", got, want)
	}
	// Unknown emails must pay the cost of a current hash.
	if p, _, _, err := parseHash(s.dummyHash); err != nil || p != defaultParams {
		t.Errorf("dummy hash params = %+v, err = %v; want defaultParams", p, err)
	}
}

// Registration shares the limiter with login: it can't hash while every slot
// is taken, and gives up (writing nothing) when its context ends.
func TestRegisterWaitsForHashSlot(t *testing.T) {
	rec := &email.Recorder{}
	s := newTestService(t, rec)
	s.hashSlots = make(chan struct{}, 1)
	s.hashSlots <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := s.Register(ctx, "ana@example.com", testPassword); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
	s.Wait()
	if n := countRows(t, s.pool, `SELECT count(*) FROM users`); n != 0 {
		t.Errorf("users = %d, want 0", n)
	}
	if n := len(rec.Messages()); n != 0 {
		t.Errorf("sent %d emails, want 0", n)
	}
}
