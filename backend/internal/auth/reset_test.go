package auth

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"vocatogether/backend/internal/email"
	"vocatogether/backend/internal/ratelimit"
)

const newTestPassword = "violet-harbor-82-comet"

// seedResetToken issues a password reset token for addr through the store
// (no email) and returns it.
func seedResetToken(t *testing.T, pool *pgxpool.Pool, addr string) Token {
	t.Helper()
	tok := NewToken("")
	if _, r, err := issuePasswordResetToken(context.Background(), pool, addr, tok.Hash, passwordResetTokenTTL); err != nil || r != resetIssued {
		t.Fatalf("seed reset token: r=%v err=%v", r, err)
	}
	return tok
}

// resetFixture is a verified account for ana@example.com with password
// testPassword, two logged-in sessions and a live reset token.
type resetFixture struct {
	loginFixture
	rec      *email.Recorder
	sessions []Credentials
	token    Token
}

func newResetFixture(t *testing.T) resetFixture {
	t.Helper()
	rec := &email.Recorder{}
	s := newTestService(t, rec)
	id := seedAccount(t, s.pool, "ana@example.com", HashPassword(testPassword), true)
	f := resetFixture{loginFixture: loginFixture{testService: s, userID: id}, rec: rec}
	for range 2 {
		c, err := f.Login(context.Background(), "ana@example.com", testPassword, testUserAgent)
		if err != nil {
			t.Fatal(err)
		}
		f.sessions = append(f.sessions, c)
	}
	f.token = seedResetToken(t, s.pool, "ana@example.com")
	return f
}

func (f resetFixture) passwordHash(t *testing.T) string {
	t.Helper()
	return loadUser(t, f.pool, "ana@example.com").passwordHash
}

func liveSessions(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	return countRows(t, pool, `SELECT count(*) FROM sessions WHERE revoked_at IS NULL`)
}

// requireNothingReset checks that a failed reset left the account as it was:
// the token still usable, the old password in place, no session revoked.
func (f resetFixture) requireNothingReset(t *testing.T, hashBefore string) {
	t.Helper()
	if tokenUsed(t, f.pool, f.token) {
		t.Error("token consumed by a failed reset")
	}
	if got := f.passwordHash(t); got != hashBefore {
		t.Error("password hash changed by a failed reset")
	}
	if n := liveSessions(t, f.pool); n != len(f.sessions) {
		t.Errorf("live sessions = %d, want %d", n, len(f.sessions))
	}
}

func requirePasswordIs(t *testing.T, pool *pgxpool.Pool, addr, password string) {
	t.Helper()
	ok, _, err := VerifyPassword(loadUser(t, pool, addr).passwordHash, password)
	if err != nil || !ok {
		t.Fatalf("stored hash doesn't match %q: ok=%v err=%v", password, ok, err)
	}
}

func requireFieldErrors(t *testing.T, err error, want ...*FieldError) {
	t.Helper()
	var verr *ValidationError
	if !errors.As(err, &verr) || !slices.Equal(verr.Fields, want) {
		t.Fatalf("err = %v, want validation error %v", err, want)
	}
}

// requireInternal checks that err is neither nil nor a client error.
func requireInternal(t *testing.T, err error) {
	t.Helper()
	var verr *ValidationError
	if err == nil || errors.As(err, &verr) {
		t.Fatalf("err = %v, want an internal error", err)
	}
}

// saturateHashSlots takes the only hash slot, so any argon2 work blocks until
// the caller's context ends. Validation that returns before hashing is
// unaffected, which proves no hashing was attempted.
func saturateHashSlots(s testService) {
	s.hashSlots = make(chan struct{}, 1)
	s.hashSlots <- struct{}{}
}

func beginTx(t *testing.T, pool *pgxpool.Pool) pgx.Tx {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	return tx
}

// requireResetLogsClean checks the service log for every secret reset
// handles: passwords, email and the reset token with its hash.
func requireResetLogsClean(t *testing.T, s testService, tokens ...Token) {
	t.Helper()
	s.Wait()
	secrets := []string{testPassword, newTestPassword, "ana@example.com"}
	for _, tok := range tokens {
		secrets = append(secrets, tokenSecrets(tok.Raw)...)
	}
	requireNoSecrets(t, "log", s.logs.String(), secrets)
}

// ---- Forgot password ----

func TestForgotPassword(t *testing.T) {
	rec := &email.Recorder{}
	s := newTestService(t, rec)
	userID := seedAccount(t, s.pool, "ana@example.com", HashPassword(testPassword), true)

	if err := s.ForgotPassword(context.Background(), " Ana@Example.COM "); err != nil {
		t.Fatal(err)
	}
	s.Wait()

	msgs := rec.Messages()
	if len(msgs) != 1 {
		t.Fatalf("sent %d emails, want 1", len(msgs))
	}
	raw := tokenIn(t, msgs[0])
	if msgs[0].To != "ana@example.com" || !strings.HasPrefix(msgs[0].Subject, "Reset") {
		t.Errorf("email = %#v, want the reset email to ana@example.com", msgs[0])
	}
	if link := linkIn(t, msgs[0].Text); link.Path != resetPasswordPath || len(link.Query()) != 1 {
		t.Errorf("link = %s, want %s with only the token", link, resetPasswordPath)
	}
	var lifetime time.Duration
	err := s.pool.QueryRow(context.Background(),
		`SELECT expires_at - created_at FROM user_tokens
		 WHERE token_hash = $1 AND purpose = $2 AND user_id = $3 AND used_at IS NULL`,
		HashToken(raw), PurposePasswordReset, userID).Scan(&lifetime)
	if err != nil {
		t.Fatalf("emailed token not stored as a live reset token: %v", err)
	}
	if lifetime != passwordResetTokenTTL {
		t.Errorf("token lifetime = %v, want %v", lifetime, passwordResetTokenTTL)
	}
	if !strings.Contains(s.logs.String(), "auth: password reset requested") || !strings.Contains(s.logs.String(), userID) {
		t.Errorf("log lacks the request with user_id: %s", s.logs)
	}
	requireResetLogsClean(t, s, Token{Raw: raw})
	requireNoSecrets(t, "database", dumpTables(t, s.pool), []string{raw})
}

// An unverified account can reset too: its owner may be locked out of an
// address someone else registered (decision 017).
func TestForgotPasswordUnverifiedAccount(t *testing.T) {
	rec := &email.Recorder{}
	s := newTestService(t, rec)
	verification := seedUser(t, s.pool, "ana@example.com")

	if err := s.ForgotPassword(context.Background(), "ana@example.com"); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	if n := len(rec.Messages()); n != 1 {
		t.Fatalf("sent %d emails, want 1", n)
	}
	if tokenUsed(t, s.pool, verification) {
		t.Error("verification token consumed by forgot-password")
	}
	if n := unusedTokens(t, s.pool, "ana@example.com"); n != 2 {
		t.Errorf("unused tokens = %d, want 2 (verification and reset)", n)
	}
}

func TestForgotPasswordUnknownEmail(t *testing.T) {
	rec := &email.Recorder{}
	s := newTestService(t, rec)

	if err := s.ForgotPassword(context.Background(), "nobody@example.com"); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	if n := len(rec.Messages()); n != 0 {
		t.Errorf("sent %d emails, want 0", n)
	}
	if n := countRows(t, s.pool, `SELECT count(*) FROM user_tokens`); n != 0 {
		t.Errorf("tokens = %d, want 0", n)
	}
	if !strings.Contains(s.logs.String(), "auth: password reset request had no effect") {
		t.Errorf("log lacks the no-effect line: %s", s.logs)
	}
	requireNoSecrets(t, "log", s.logs.String(), []string{"nobody@example.com"})
}

// An account created with Google has no password and can't be recovered
// through the mailbox (decision 020): a third-party address may have changed
// owner since Google verified it. Its owner is told to use Google instead.
func TestForgotPasswordPasswordlessAccount(t *testing.T) {
	rec := &email.Recorder{}
	s := newTestService(t, rec)
	userID := seedGoogleAccount(t, s.pool, "gina@example.com", "1001")

	if err := s.ForgotPassword(context.Background(), " Gina@Example.com "); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	if msgs := rec.Messages(); len(msgs) != 1 || msgs[0] != passwordlessAccountEmail("gina@example.com") {
		t.Fatalf("emails = %v, want only the passwordless-account notice", msgs)
	}
	if n := countRows(t, s.pool, `SELECT count(*) FROM user_tokens`); n != 0 {
		t.Errorf("tokens = %d, want 0", n)
	}
	if logs := s.logs.String(); !strings.Contains(logs, "auth: password reset requested for passwordless account") ||
		!strings.Contains(logs, userID) {
		t.Errorf("log lacks the passwordless request with user_id: %s", logs)
	}
	requireNoSecrets(t, "log", s.logs.String(), []string{"gina@example.com"})
}

// The notice spends the per-account mail limit like any other email.
func TestForgotPasswordPasswordlessAccountIsRateLimited(t *testing.T) {
	rec := &email.Recorder{}
	s := newTestService(t, rec)
	s.limits.Mail = ratelimit.New[[32]byte]("account_mail", 1, time.Hour, 10, slog.New(slog.DiscardHandler))
	seedGoogleAccount(t, s.pool, "gina@example.com", "1001")

	if err := s.ForgotPassword(context.Background(), "gina@example.com"); err != nil {
		t.Fatal(err)
	}
	var limited *RateLimitedError
	if err := s.ForgotPassword(context.Background(), "gina@example.com"); !errors.As(err, &limited) {
		t.Fatalf("second request: err = %v, want *RateLimitedError", err)
	}
	s.Wait()
	if n := len(rec.Messages()); n != 1 {
		t.Errorf("sent %d emails, want 1", n)
	}
}

func TestForgotPasswordInvalidEmailWithoutDatabase(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	s.pool.Close()
	for _, input := range []string{"", "not-an-email", "ana@example.com\r\nBcc: x@y.z"} {
		requireFieldErrors(t, s.ForgotPassword(context.Background(), input), ErrEmailInvalid)
	}
}

func TestForgotPasswordReplacesPreviousToken(t *testing.T) {
	rec := &email.Recorder{}
	s := newTestService(t, rec)
	seedAccount(t, s.pool, "ana@example.com", HashPassword(testPassword), true)

	for range 2 {
		if err := s.ForgotPassword(context.Background(), "ana@example.com"); err != nil {
			t.Fatal(err)
		}
	}
	s.Wait()
	msgs := rec.Messages()
	if len(msgs) != 2 {
		t.Fatalf("sent %d emails, want 2", len(msgs))
	}
	if n := unusedTokens(t, s.pool, "ana@example.com"); n != 1 {
		t.Errorf("unused tokens = %d, want 1", n)
	}
	requireTokenInvalid(t, s.ResetPassword(context.Background(), tokenIn(t, msgs[0]), newTestPassword))
	if err := s.ResetPassword(context.Background(), tokenIn(t, msgs[1]), newTestPassword); err != nil {
		t.Fatalf("latest token: %v", err)
	}
}

// Concurrent requests serialize on the user row: exactly one token stays
// live, and it is one of those emailed.
func TestForgotPasswordConcurrent(t *testing.T) {
	rec := &email.Recorder{}
	s := newTestService(t, rec)
	seedAccount(t, s.pool, "ana@example.com", HashPassword(testPassword), true)

	const n = 10
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			if err := s.ForgotPassword(context.Background(), "ana@example.com"); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	s.Wait()

	if got := unusedTokens(t, s.pool, "ana@example.com"); got != 1 {
		t.Fatalf("unused tokens = %d, want 1", got)
	}
	msgs := rec.Messages()
	if len(msgs) != n {
		t.Fatalf("sent %d emails, want %d", len(msgs), n)
	}
	live := 0
	for _, msg := range msgs {
		if _, found, err := findPasswordResetOwner(context.Background(), s.pool, HashToken(tokenIn(t, msg))); err != nil {
			t.Fatal(err)
		} else if found {
			live++
		}
	}
	if live != 1 {
		t.Errorf("live emailed tokens = %d, want 1", live)
	}
}

func TestForgotPasswordSendsOnlyAfterCommit(t *testing.T) {
	var s testService
	var committed bool
	s = newTestService(t, senderFunc(func(ctx context.Context, msg email.Message) error {
		// A different connection sees the token only once it is committed.
		_, found, err := findPasswordResetOwner(ctx, s.pool, HashToken(tokenIn(t, msg)))
		committed = err == nil && found
		return err
	}))
	seedAccount(t, s.pool, "ana@example.com", HashPassword(testPassword), true)

	if err := s.ForgotPassword(context.Background(), "ana@example.com"); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	if !committed {
		t.Error("email sent before the token was committed")
	}
}

func TestForgotPasswordEmailOutlivesRequestContext(t *testing.T) {
	rec := &email.Recorder{} // rejects cancelled contexts
	s := newTestService(t, rec)
	seedAccount(t, s.pool, "ana@example.com", HashPassword(testPassword), true)

	ctx, cancel := context.WithCancel(context.Background())
	if err := s.ForgotPassword(ctx, "ana@example.com"); err != nil {
		t.Fatal(err)
	}
	cancel()
	s.Wait()
	if n := len(rec.Messages()); n != 1 {
		t.Errorf("sent %d emails after the request ended, want 1", n)
	}
}

func TestForgotPasswordEmailFailureKeepsToken(t *testing.T) {
	var sent email.Message
	s := newTestService(t, senderFunc(func(ctx context.Context, msg email.Message) error {
		sent = msg
		return errors.New("provider unavailable")
	}))
	seedAccount(t, s.pool, "ana@example.com", HashPassword(testPassword), true)

	if err := s.ForgotPassword(context.Background(), "ana@example.com"); err != nil {
		t.Fatalf("err = %v, want nil: a failed send must not fail the request", err)
	}
	s.Wait()
	raw := tokenIn(t, sent)
	if _, found, err := findPasswordResetOwner(context.Background(), s.pool, HashToken(raw)); err != nil || !found {
		t.Errorf("token not kept after a failed send: found=%v err=%v", found, err)
	}
	logs := s.logs.String()
	if !strings.Contains(logs, "auth: send email failed") || !strings.Contains(logs, "kind=password_reset") {
		t.Errorf("send failure not logged with its kind: %s", logs)
	}
	requireResetLogsClean(t, s, Token{Raw: raw})
}

func TestForgotPasswordDatabaseFailureKeepsOldToken(t *testing.T) {
	rec := &email.Recorder{}
	s := newTestService(t, rec)
	seedAccount(t, s.pool, "ana@example.com", HashPassword(testPassword), true)
	old := seedResetToken(t, s.pool, "ana@example.com")
	injectFailure(t, s.pool, "BEFORE INSERT ON user_tokens")

	err := s.ForgotPassword(context.Background(), "ana@example.com")
	requireInternal(t, err)
	requireNoSecrets(t, "error", err.Error(), []string{"ana@example.com"})
	s.Wait()
	if n := len(rec.Messages()); n != 0 {
		t.Errorf("sent %d emails after a rollback, want 0", n)
	}
	if _, found, err := findPasswordResetOwner(context.Background(), s.pool, old.Hash); err != nil || !found {
		t.Errorf("old token lost by a rolled-back request: found=%v err=%v", found, err)
	}
}

func TestForgotPasswordHonorsCancelledContext(t *testing.T) {
	rec := &email.Recorder{}
	s := newTestService(t, rec)
	seedAccount(t, s.pool, "ana@example.com", HashPassword(testPassword), true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	requireErrorIs(t, s.ForgotPassword(ctx, "ana@example.com"), context.Canceled)
	s.Wait()
	if n := len(rec.Messages()); n != 0 {
		t.Errorf("sent %d emails, want 0", n)
	}
	if n := unusedTokens(t, s.pool, "ana@example.com"); n != 0 {
		t.Errorf("unused tokens = %d, want 0", n)
	}
}

// ---- Reset password ----

func TestResetPassword(t *testing.T) {
	f := newResetFixture(t)
	ctx := context.Background()
	before := loadUser(t, f.pool, "ana@example.com")

	if err := f.ResetPassword(ctx, f.token.Raw, newTestPassword); err != nil {
		t.Fatal(err)
	}

	after := loadUser(t, f.pool, "ana@example.com")
	requirePasswordIs(t, f.pool, "ana@example.com", newTestPassword)
	if !after.updatedAt.After(before.updatedAt) {
		t.Error("updated_at not advanced")
	}
	if !after.verifiedAt.Equal(*before.verifiedAt) {
		t.Error("email_verified_at of a verified account changed")
	}
	if !tokenUsed(t, f.pool, f.token) {
		t.Error("token not marked used")
	}
	if n := liveSessions(t, f.pool); n != 0 {
		t.Errorf("live sessions = %d, want 0", n)
	}

	// Every credential issued before the reset is dead.
	for _, c := range f.sessions {
		id, err := f.Authenticate(ctx, c.AccessToken.Raw)
		requireInvalidAccess(t, id, err)
		res, err := f.Refresh(ctx, c.RefreshToken.Raw)
		requireInvalidRefresh(t, res, err)
	}
	_, err := f.Login(ctx, "ana@example.com", testPassword, "")
	requireErrorIs(t, err, ErrInvalidCredentials)

	// The new password logs in, and the new session works.
	c, err := f.Login(ctx, "ana@example.com", newTestPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Authenticate(ctx, c.AccessToken.Raw); err != nil {
		t.Errorf("new session rejected: %v", err)
	}

	f.Wait()
	msgs := f.rec.Messages()
	if len(msgs) != 1 || msgs[0] != passwordChangedEmail("ana@example.com") {
		t.Errorf("emails = %v, want only the password-changed email", msgs)
	}
	logs := f.logs.String()
	if !strings.Contains(logs, "auth: password reset succeeded") || !strings.Contains(logs, "sessions_revoked=2") {
		t.Errorf("log lacks the success with the revoked count: %s", logs)
	}
	requireResetLogsClean(t, f.testService, f.token)
	requireNoSecrets(t, "database", dumpTables(t, f.pool), []string{f.token.Raw, testPassword, newTestPassword})
}

// Completing a reset proves control of the mailbox, like verification, and
// no other one-time token of the account survives it.
func TestResetPasswordVerifiesUnverifiedAccount(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	verification := seedUser(t, s.pool, "ana@example.com")
	tok := seedResetToken(t, s.pool, "ana@example.com")

	if err := s.ResetPassword(context.Background(), tok.Raw, newTestPassword); err != nil {
		t.Fatal(err)
	}
	if loadUser(t, s.pool, "ana@example.com").verifiedAt == nil {
		t.Error("email_verified_at not set")
	}
	if n := unusedTokens(t, s.pool, "ana@example.com"); n != 0 {
		t.Errorf("unused tokens = %d, want 0", n)
	}
	requireTokenInvalid(t, s.VerifyEmail(context.Background(), verification.Raw))
	if _, err := s.Login(context.Background(), "ana@example.com", newTestPassword, ""); err != nil {
		t.Errorf("login after reset: %v", err)
	}
}

// Forgot-password never issues a reset token to a passwordless account; if
// one existed anyway, it still couldn't set a password (decision 020). It is
// rejected before any argon2 work, and the store refuses it on its own too.
func TestResetPasswordRefusesPasswordlessAccount(t *testing.T) {
	rec := &email.Recorder{}
	s := newTestService(t, rec)
	userID := seedGoogleAccount(t, s.pool, "gina@example.com", "1001")
	tok := NewToken("")
	if _, err := s.pool.Exec(context.Background(),
		`INSERT INTO user_tokens (user_id, purpose, token_hash, expires_at) VALUES ($1, $2, $3, now() + interval '30 minutes')`,
		userID, PurposePasswordReset, tok.Hash); err != nil {
		t.Fatal(err)
	}

	saturateHashSlots(s)
	requireTokenInvalid(t, s.ResetPassword(context.Background(), tok.Raw, newTestPassword))

	r, err := resetPassword(context.Background(), s.pool, tok.Hash, HashPassword(newTestPassword))
	if err != nil || r.ok {
		t.Fatalf("store reset of a passwordless account: %+v err=%v; want not ok", r, err)
	}
	if tokenUsed(t, s.pool, tok) {
		t.Error("token consumed")
	}
	if h := loadUser(t, s.pool, "gina@example.com").passwordHash; h != "" {
		t.Error("a password was set on a passwordless account")
	}
	s.Wait()
	if n := len(rec.Messages()); n != 0 {
		t.Errorf("sent %d emails, want 0", n)
	}
}

func TestResetPasswordRejectsMalformedTokensWithoutDatabase(t *testing.T) {
	f := newResetFixture(t)
	f.pool.Close()
	valid := NewToken("").Raw
	for name, raw := range map[string]string{
		"empty":         "",
		"junk":          "not-a-token",
		"access token":  f.sessions[0].AccessToken.Raw,
		"refresh token": f.sessions[0].RefreshToken.Raw,
		"padded":        valid + "=",
		"truncated":     valid[:len(valid)-1],
		"with space":    " " + valid,
	} {
		t.Run(name, func(t *testing.T) {
			requireTokenInvalid(t, f.ResetPassword(context.Background(), raw, newTestPassword))
		})
	}
	if !strings.Contains(f.logs.String(), "reason=malformed") {
		t.Errorf("log lacks reason=malformed: %s", f.logs)
	}
}

// Unusable tokens are rejected before any argon2 work (the only hash slot is
// taken, so hashing would block) and change nothing.
func TestResetPasswordRejectsUnusableTokens(t *testing.T) {
	cases := map[string]func(t *testing.T, f resetFixture) string{
		"unknown": func(t *testing.T, f resetFixture) string { return NewToken("").Raw },
		"expired": func(t *testing.T, f resetFixture) string {
			if _, err := f.pool.Exec(context.Background(),
				`UPDATE user_tokens SET expires_at = now() - interval '1 second' WHERE token_hash = $1`, f.token.Hash); err != nil {
				t.Fatal(err)
			}
			return f.token.Raw
		},
		"replaced": func(t *testing.T, f resetFixture) string {
			old := f.token.Raw
			seedResetToken(t, f.pool, "ana@example.com")
			return old
		},
		"verification token": func(t *testing.T, f resetFixture) string {
			return seedUser(t, f.pool, "bea@example.com").Raw
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			f := newResetFixture(t)
			raw := setup(t, f)
			hash := f.passwordHash(t)
			saturateHashSlots(f.testService)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			requireTokenInvalid(t, f.ResetPassword(ctx, raw, newTestPassword))
			if got := f.passwordHash(t); got != hash {
				t.Error("password changed")
			}
			if n := liveSessions(t, f.pool); n != 2 {
				t.Errorf("live sessions = %d, want 2", n)
			}
			if !strings.Contains(f.logs.String(), "reason=invalid_token") {
				t.Errorf("log lacks reason=invalid_token: %s", f.logs)
			}
			requireResetLogsClean(t, f.testService, f.token, Token{Raw: raw})
		})
	}
}

func TestResetPasswordVerificationTokenIsNotConsumed(t *testing.T) {
	f := newResetFixture(t)
	verification := seedUser(t, f.pool, "bea@example.com")
	requireTokenInvalid(t, f.ResetPassword(context.Background(), verification.Raw, newTestPassword))
	if tokenUsed(t, f.pool, verification) {
		t.Error("verification token consumed by reset")
	}
}

func TestResetPasswordTokenIsSingleUse(t *testing.T) {
	f := newResetFixture(t)
	if err := f.ResetPassword(context.Background(), f.token.Raw, newTestPassword); err != nil {
		t.Fatal(err)
	}
	requireTokenInvalid(t, f.ResetPassword(context.Background(), f.token.Raw, "another-long-passphrase-9"))
	requirePasswordIs(t, f.pool, "ana@example.com", newTestPassword)
}

// A password that fails the policy must not burn the link: the user fixes a
// typo and submits again. No argon2 work is done for it.
func TestResetPasswordPolicyFailuresKeepToken(t *testing.T) {
	for _, tt := range []struct {
		password string
		want     *FieldError
	}{
		{"", ErrPasswordTooShort},
		{"short", ErrPasswordTooShort},
		{strings.Repeat("x", PasswordMaxLength+1), ErrPasswordTooLong},
		{"password123", ErrPasswordTooCommon},
		{"Ana@Example.com", ErrPasswordSameAsEmail},
		// Six "n" + combining tilde: 12 code points, 6 after NFKC.
		{strings.Repeat("ñ", 6), ErrPasswordTooShort},
	} {
		t.Run(tt.want.Code, func(t *testing.T) {
			f := newResetFixture(t)
			hash := f.passwordHash(t)
			saturateHashSlots(f.testService)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			requireFieldErrors(t, f.ResetPassword(ctx, f.token.Raw, tt.password), tt.want)
			f.requireNothingReset(t, hash)
		})
	}
}

func TestResetPasswordReportsAllInvalidFields(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	s.pool.Close()
	requireFieldErrors(t, s.ResetPassword(context.Background(), "junk", "short"), ErrTokenInvalid, ErrPasswordTooShort)
}

// The new password is NFKC-normalized like at registration (decision 009):
// it is set with a decomposed "ñ" and logs in with the precomposed one.
func TestResetPasswordNormalizesPassword(t *testing.T) {
	f := newResetFixture(t)
	if err := f.ResetPassword(context.Background(), f.token.Raw, "contraseña-segura-7"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Login(context.Background(), "ana@example.com", "contraseña-segura-7", ""); err != nil {
		t.Errorf("login with the precomposed form: %v", err)
	}
}

// Every step of the reset transaction is atomic with the others: a failure
// at any of them leaves everything as it was and sends nothing.
func TestResetPasswordFailureChangesNothing(t *testing.T) {
	for _, event := range []string{
		"BEFORE UPDATE ON user_tokens",
		"BEFORE UPDATE ON users",
		"BEFORE UPDATE ON sessions",
		"BEFORE DELETE ON user_tokens",
	} {
		t.Run(event, func(t *testing.T) {
			f := newResetFixture(t)
			// A pending token of another purpose, so the DELETE has a row.
			if _, err := f.pool.Exec(context.Background(),
				`INSERT INTO user_tokens (user_id, purpose, token_hash, expires_at)
				 VALUES ($1, $2, $3, now() + interval '1 hour')`,
				f.userID, PurposeEmailVerification, NewToken("").Hash); err != nil {
				t.Fatal(err)
			}
			hash := f.passwordHash(t)
			injectFailure(t, f.pool, event)

			err := f.ResetPassword(context.Background(), f.token.Raw, newTestPassword)
			requireInternal(t, err)
			requireNoSecrets(t, "error", err.Error(), append(tokenSecrets(f.token.Raw), newTestPassword, "ana@example.com"))
			f.requireNothingReset(t, hash)
			if n := unusedTokens(t, f.pool, "ana@example.com"); n != 2 {
				t.Errorf("unused tokens = %d, want 2", n)
			}
			f.Wait()
			if n := len(f.rec.Messages()); n != 0 {
				t.Errorf("sent %d emails after a rollback, want 0", n)
			}
		})
	}
}

func TestResetPasswordDatabaseFailure(t *testing.T) {
	f := newResetFixture(t)
	f.pool.Close()
	err := f.ResetPassword(context.Background(), f.token.Raw, newTestPassword)
	requireInternal(t, err)
	requireNoSecrets(t, "error", err.Error(), append(tokenSecrets(f.token.Raw), newTestPassword))
}

func TestResetPasswordHonorsCancelledContext(t *testing.T) {
	f := newResetFixture(t)
	hash := f.passwordHash(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	requireErrorIs(t, f.ResetPassword(ctx, f.token.Raw, newTestPassword), context.Canceled)
	f.requireNothingReset(t, hash)
}

// Reset shares the argon2 limiter with register and login, and gives up
// (consuming nothing) when its context ends while waiting for a slot.
func TestResetPasswordWaitsForHashSlot(t *testing.T) {
	f := newResetFixture(t)
	hash := f.passwordHash(t)
	saturateHashSlots(f.testService)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	requireErrorIs(t, f.ResetPassword(ctx, f.token.Raw, newTestPassword), context.DeadlineExceeded)
	f.requireNothingReset(t, hash)
}

func TestResetPasswordCancelledWhileWaitingForUserLock(t *testing.T) {
	f := newResetFixture(t)
	hash := f.passwordHash(t)
	tx := lockUser(t, f.pool, "ana@example.com")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.ResetPassword(ctx, f.token.Raw, newTestPassword) }()
	waitForLockWaiters(t, f.pool, 1)
	cancel()
	requireErrorIs(t, <-done, context.Canceled)
	if err := tx.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.requireNothingReset(t, hash)
}

func TestResetPasswordEmailFailureKeepsReset(t *testing.T) {
	f := newResetFixture(t)
	f.rec.SetError(errors.New("provider unavailable"))

	if err := f.ResetPassword(context.Background(), f.token.Raw, newTestPassword); err != nil {
		t.Fatalf("err = %v, want nil: a failed send must not undo a committed reset", err)
	}
	f.Wait()
	requirePasswordIs(t, f.pool, "ana@example.com", newTestPassword)
	if n := liveSessions(t, f.pool); n != 0 {
		t.Errorf("live sessions = %d, want 0", n)
	}
	if !strings.Contains(f.logs.String(), "kind=password_changed") {
		t.Errorf("send failure not logged with its kind: %s", f.logs)
	}
	requireResetLogsClean(t, f.testService, f.token)
}

func TestResetPasswordSendsOnlyAfterCommit(t *testing.T) {
	var f resetFixture
	var committed bool
	rec := &email.Recorder{}
	s := newTestService(t, senderFunc(func(ctx context.Context, msg email.Message) error {
		// A different connection sees the new password only once committed.
		var stored string
		err := f.pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE email = 'ana@example.com'`).Scan(&stored)
		ok, _, _ := VerifyPassword(stored, newTestPassword)
		committed = err == nil && ok
		return rec.Send(ctx, msg) // rejects the finished request's context
	}))
	f = resetFixture{loginFixture: loginFixture{testService: s}, rec: rec}
	seedAccount(t, s.pool, "ana@example.com", HashPassword(testPassword), true)
	f.token = seedResetToken(t, s.pool, "ana@example.com")

	ctx, cancel := context.WithCancel(context.Background())
	if err := f.ResetPassword(ctx, f.token.Raw, newTestPassword); err != nil {
		t.Fatal(err)
	}
	cancel()
	f.Wait()
	if !committed {
		t.Error("email sent before the reset was committed")
	}
	if n := len(rec.Messages()); n != 1 {
		t.Errorf("sent %d emails after the request ended, want 1", n)
	}
}

// ---- Races ----
//
// Each test holds a real lock in a manual transaction and waits until the
// other side blocks on it, so the order is deterministic and the test fails
// if the lock or the statement order it relies on is removed.

// Two resets with one token: the second waits for the user lock, then its
// conditional UPDATE sees the token used.
func TestResetPasswordConcurrentSameTokenSucceedsOnce(t *testing.T) {
	f := newResetFixture(t)
	ctx := context.Background()
	firstHash := HashPassword(testPassword + "-first")

	tx := beginTx(t, f.pool)
	if r, err := resetPasswordTx(ctx, tx, f.token.Hash, firstHash); err != nil || !r.ok {
		t.Fatalf("first reset: %+v, %v", r, err)
	}
	done := make(chan error, 1)
	go func() { done <- f.ResetPassword(ctx, f.token.Raw, newTestPassword) }()
	waitForLockWaiters(t, f.pool, 1)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	requireTokenInvalid(t, <-done)
	if got := f.passwordHash(t); got != firstHash {
		t.Error("second reset overwrote the first")
	}
}

// Lock order (store.go): reset must lock the user row before any token row.
// A transaction holding the user row, like forgot-password, can then still
// delete the reset token while reset waits. If reset locked the token row
// first and the user row second, the two would deadlock here (and this
// DELETE would hit its lock timeout).
func TestResetPasswordLocksUserBeforeTokenRow(t *testing.T) {
	f := newResetFixture(t)
	ctx := context.Background()

	tx := lockUser(t, f.pool, "ana@example.com")
	done := make(chan error, 1)
	go func() { done <- f.ResetPassword(ctx, f.token.Raw, newTestPassword) }()
	waitForLockWaiters(t, f.pool, 1)
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '2s'`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM user_tokens WHERE token_hash = $1`, f.token.Hash); err != nil {
		t.Fatalf("token row locked by a reset still waiting for the user row: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	requireTokenInvalid(t, <-done)
}

// If the first reset rolls back, the waiting one sees the token unused.
func TestResetPasswordWaitingForRolledBackResetSucceeds(t *testing.T) {
	f := newResetFixture(t)
	ctx := context.Background()

	tx := beginTx(t, f.pool)
	if r, err := resetPasswordTx(ctx, tx, f.token.Hash, HashPassword("rolled-back-password")); err != nil || !r.ok {
		t.Fatalf("first reset: %+v, %v", r, err)
	}
	done := make(chan error, 1)
	go func() { done <- f.ResetPassword(ctx, f.token.Raw, newTestPassword) }()
	waitForLockWaiters(t, f.pool, 1)
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	if err := <-done; err != nil {
		t.Fatal(err)
	}
	requirePasswordIs(t, f.pool, "ana@example.com", newTestPassword)
}

// A login that verified the old password and holds the user row FOR SHARE
// while inserting its session makes reset wait; reset then revokes that
// session too, because its UPDATE runs after the login committed.
func TestResetPasswordRevokesSessionOfConcurrentLogin(t *testing.T) {
	f := newResetFixture(t)
	ctx := context.Background()
	oldHash := f.passwordHash(t)

	tx := beginTx(t, f.pool)
	var one int
	if err := tx.QueryRow(ctx, `SELECT 1 FROM users WHERE id = $1 AND password_hash = $2 FOR SHARE`,
		f.userID, oldHash).Scan(&one); err != nil {
		t.Fatal(err)
	}
	access := NewToken(AccessTokenPrefix)
	if _, err := tx.Exec(ctx,
		`INSERT INTO sessions (user_id, access_token_hash, access_expires_at, refresh_token_hash, refresh_expires_at, expires_at)
		 VALUES ($1, $2, now() + interval '15 minutes', $3, now() + interval '1 day', now() + interval '1 day')`,
		f.userID, access.Hash, NewToken(RefreshTokenPrefix).Hash); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- f.ResetPassword(ctx, f.token.Raw, newTestPassword) }()
	waitForLockWaiters(t, f.pool, 1)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	if err := <-done; err != nil {
		t.Fatal(err)
	}
	id, err := f.Authenticate(ctx, access.Raw)
	requireInvalidAccess(t, id, err)
	if n := liveSessions(t, f.pool); n != 0 {
		t.Errorf("live sessions = %d, want 0", n)
	}
}

// A login that verified the old password but reaches its re-check while a
// reset holds the user row waits, then sees the new hash and creates nothing.
func TestLoginWaitingForResetCreatesNothing(t *testing.T) {
	f := newResetFixture(t)
	ctx := context.Background()
	oldHash := f.passwordHash(t)

	tx := beginTx(t, f.pool)
	if r, err := resetPasswordTx(ctx, tx, f.token.Hash, HashPassword(newTestPassword)); err != nil || !r.ok {
		t.Fatalf("reset: %+v, %v", r, err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := createSession(ctx, f.pool, f.userID, oldHash, NewToken(AccessTokenPrefix).Hash, NewToken(RefreshTokenPrefix).Hash, nil)
		done <- err
	}()
	waitForLockWaiters(t, f.pool, 1)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	if err := <-done; !errors.Is(err, errPasswordChanged) {
		t.Fatalf("err = %v, want errPasswordChanged", err)
	}
	if n := liveSessions(t, f.pool); n != 0 {
		t.Errorf("live sessions = %d, want 0", n)
	}
}

// A reset of a token that a concurrent forgot-password is replacing waits
// for the user lock and then finds the token gone.
func TestResetPasswordOfTokenReplacedConcurrently(t *testing.T) {
	f := newResetFixture(t)
	ctx := context.Background()
	hash := f.passwordHash(t)

	replacement := NewToken("")
	tx := beginTx(t, f.pool)
	if _, r, err := issuePasswordResetTokenTx(ctx, tx, "ana@example.com", replacement.Hash, passwordResetTokenTTL); err != nil || r != resetIssued {
		t.Fatalf("issue: %v, %v", r, err)
	}
	done := make(chan error, 1)
	go func() { done <- f.ResetPassword(ctx, f.token.Raw, newTestPassword) }()
	waitForLockWaiters(t, f.pool, 1)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	requireTokenInvalid(t, <-done)
	if got := f.passwordHash(t); got != hash {
		t.Error("password changed with a replaced token")
	}
	if err := f.ResetPassword(ctx, replacement.Raw, newTestPassword); err != nil {
		t.Errorf("replacement token: %v", err)
	}
}

// A forgot-password that waits for a reset issues a fresh token afterwards.
func TestForgotPasswordWaitingForResetIssuesFreshToken(t *testing.T) {
	f := newResetFixture(t)
	ctx := context.Background()

	tx := beginTx(t, f.pool)
	if r, err := resetPasswordTx(ctx, tx, f.token.Hash, HashPassword(newTestPassword)); err != nil || !r.ok {
		t.Fatalf("reset: %+v, %v", r, err)
	}
	done := make(chan error, 1)
	go func() { done <- f.ForgotPassword(ctx, "ana@example.com") }()
	waitForLockWaiters(t, f.pool, 1)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	if err := <-done; err != nil {
		t.Fatal(err)
	}
	f.Wait()
	msgs := f.rec.Messages()
	if len(msgs) != 1 {
		t.Fatalf("sent %d emails, want 1", len(msgs))
	}
	if err := f.ResetPassword(ctx, tokenIn(t, msgs[0]), "another-long-passphrase-9"); err != nil {
		t.Errorf("fresh token: %v", err)
	}
}

// A rotation in progress holds the session row: reset waits for it, then
// revokes the rotated session, so the freshly issued tokens die too.
func TestResetPasswordRevokesSessionRotatedConcurrently(t *testing.T) {
	f := newResetFixture(t)
	ctx := context.Background()

	access, refresh := NewToken(AccessTokenPrefix), NewToken(RefreshTokenPrefix)
	tx := beginTx(t, f.pool)
	if r, err := rotateRefreshTokenTx(ctx, tx, f.sessions[0].RefreshToken.Hash, access.Hash, refresh.Hash); err != nil || r.outcome != refreshRotated {
		t.Fatalf("rotate: %+v, %v", r, err)
	}
	done := make(chan error, 1)
	go func() { done <- f.ResetPassword(ctx, f.token.Raw, newTestPassword) }()
	waitForLockWaiters(t, f.pool, 1)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	if err := <-done; err != nil {
		t.Fatal(err)
	}
	id, err := f.Authenticate(ctx, access.Raw)
	requireInvalidAccess(t, id, err)
	res, err := f.Refresh(ctx, refresh.Raw)
	requireInvalidRefresh(t, res, err)
	if n := liveSessions(t, f.pool); n != 0 {
		t.Errorf("live sessions = %d, want 0", n)
	}
}

// A refresh that reaches a session while an uncommitted reset holds it waits,
// then sees the session revoked and rotates nothing.
func TestRefreshWaitingForResetIsRejected(t *testing.T) {
	f := newResetFixture(t)
	ctx := context.Background()
	before := loadSessions(t, f.pool)[0]

	tx := beginTx(t, f.pool)
	if r, err := resetPasswordTx(ctx, tx, f.token.Hash, HashPassword(newTestPassword)); err != nil || !r.ok {
		t.Fatalf("reset: %+v, %v", r, err)
	}
	type result struct {
		c   Credentials
		err error
	}
	done := make(chan result, 1)
	go func() {
		c, err := f.Refresh(ctx, f.sessions[0].RefreshToken.Raw)
		done <- result{c, err}
	}()
	waitForLockWaiters(t, f.pool, 1)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	r := <-done
	requireInvalidRefresh(t, r.c, r.err)
	after := loadSessions(t, f.pool)[0]
	if string(after.refreshHash) != string(before.refreshHash) || after.revokedAt == nil {
		t.Error("session rotated, or not revoked, by a refresh that waited for a reset")
	}
}

// A logout waiting for a reset finds the session already revoked: 204-style
// success with no effect.
func TestLogoutWaitingForResetHasNoEffect(t *testing.T) {
	f := newResetFixture(t)
	ctx := context.Background()

	tx := beginTx(t, f.pool)
	if r, err := resetPasswordTx(ctx, tx, f.token.Hash, HashPassword(newTestPassword)); err != nil || !r.ok {
		t.Fatalf("reset: %+v, %v", r, err)
	}
	done := make(chan error, 1)
	go func() { done <- f.Logout(ctx, f.sessions[0].AccessToken.Raw) }()
	waitForLockWaiters(t, f.pool, 1)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	if err := <-done; err != nil {
		t.Fatal(err)
	}
	f.Wait()
	if !strings.Contains(f.logs.String(), "auth: logout had no effect") {
		t.Errorf("logout revoked a session the reset had already revoked: %s", f.logs)
	}
}

// Every flow that touches an account's sessions or tokens runs concurrently
// on one account. No operation may fail other than in its expected ways (in
// particular no deadlock, SQLSTATE 40P01), and afterwards no session created
// before the last successful reset may be live. Run with -race.
func TestResetPasswordConcurrentStress(t *testing.T) {
	f := newResetFixture(t)
	ctx := context.Background()
	passwords := []string{testPassword, "first-new-passphrase-1", "second-new-passphrase-2", "third-new-passphrase-3"}

	var mu sync.Mutex
	var unexpected []error
	expect := func(err error, allowed ...error) {
		if err == nil || isTokenInvalid(err) {
			return
		}
		for _, a := range allowed {
			if errors.Is(err, a) {
				return
			}
		}
		mu.Lock()
		unexpected = append(unexpected, err)
		mu.Unlock()
	}

	creds := make(chan Credentials, 256)
	stop := make(chan struct{})
	var workers sync.WaitGroup
	for i := range 3 {
		workers.Go(func() {
			for j := 0; ; j++ {
				select {
				case <-stop:
					return
				default:
				}
				c, err := f.Login(ctx, "ana@example.com", passwords[(i+j)%len(passwords)], "")
				expect(err, ErrInvalidCredentials)
				if err == nil {
					select {
					case creds <- c:
					default:
					}
				}
			}
		})
	}
	workers.Go(func() {
		for {
			select {
			case <-stop:
				return
			case c := <-creds:
				_, err := f.Refresh(ctx, c.RefreshToken.Raw)
				expect(err, ErrInvalidRefreshToken)
				expect(f.Logout(ctx, c.AccessToken.Raw))
			}
		}
	})
	// A few forgot-password requests contend for the user row with the
	// resets. Each replaces the live token, so they are spaced out and
	// limited, letting resets win between them.
	workers.Go(func() {
		for range 5 {
			select {
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
				expect(f.ForgotPassword(ctx, "ana@example.com"))
			}
		}
	})

	// Resets run one after another, each racing a duplicate with the same
	// token. A concurrent forgot-password may replace the token first, so
	// each password is retried with a fresh token until one reset wins.
	var lastToken Token
	final := testPassword
	for _, pw := range passwords[1:] {
		for attempt := 0; final != pw; attempt++ {
			if attempt == 50 {
				t.Fatalf("no reset to %q succeeded in %d attempts", pw, attempt)
			}
			tok := seedResetToken(t, f.pool, "ana@example.com")
			errs := make([]error, 2)
			var wg sync.WaitGroup
			for k := range errs {
				wg.Go(func() { errs[k] = f.ResetPassword(ctx, tok.Raw, pw) })
			}
			wg.Wait()
			succeeded := 0
			for _, err := range errs {
				expect(err)
				if err == nil {
					succeeded++
				}
			}
			if succeeded > 1 {
				t.Errorf("one token reset the password %d times", succeeded)
			}
			if succeeded == 1 {
				lastToken, final = tok, pw
			}
		}
	}
	close(stop)
	workers.Wait()
	f.Wait()

	for _, err := range unexpected {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "40P01" {
			t.Errorf("deadlock: %v", err)
		} else {
			t.Errorf("unexpected error: %v", err)
		}
	}
	requirePasswordIs(t, f.pool, "ana@example.com", final)
	// Sessions created before the last reset's transaction started must all
	// be revoked; later ones can only come from the final password.
	stale := countRows(t, f.pool,
		`SELECT count(*) FROM sessions s, user_tokens t
		 WHERE t.token_hash = $1 AND s.revoked_at IS NULL AND s.created_at < t.used_at`, lastToken.Hash)
	if stale != 0 {
		t.Errorf("%d sessions from before the last reset are still live", stale)
	}
	for _, pw := range passwords {
		if pw == final {
			continue
		}
		_, err := f.Login(ctx, "ana@example.com", pw, "")
		requireErrorIs(t, err, ErrInvalidCredentials)
	}
}
