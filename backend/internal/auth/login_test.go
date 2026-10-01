package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"vocatogether/backend/internal/email"
	"vocatogether/backend/internal/testutil"
)

// ---- User-Agent ----

func TestNormalizeUserAgent(t *testing.T) {
	long := strings.Repeat("a", 300)
	tests := map[string]struct {
		in   string
		want *string
	}{
		"empty":              {"", nil},
		"only whitespace":    {" \t ", nil},
		"only controls":      {"\x00\x01\x7f", nil},
		"typical":            {"VocaTogether/1.0 (iOS 18.1)", ptr("VocaTogether/1.0 (iOS 18.1)")},
		"trimmed":            {"  curl/8.0  ", ptr("curl/8.0")},
		"controls removed":   {"a\x00b\nc\td\x1b", ptr("abcd")},
		"C1 controls":        {"a\u0085b\u009bc", ptr("abc")},
		"invalid UTF-8":      {"ok\xff\xfeyes", ptr("okyes")},
		"unicode kept":       {"Navegador ñandú 日本", ptr("Navegador ñandú 日本")},
		"truncated to limit": {long, ptr(long[:maxUserAgentBytes])},
		"exactly the limit":  {long[:maxUserAgentBytes], ptr(long[:maxUserAgentBytes])},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := normalizeUserAgent(tt.in)
			switch {
			case got == nil && tt.want == nil:
			case got == nil || tt.want == nil || *got != *tt.want:
				t.Errorf("normalizeUserAgent(%q) = %v, want %v", tt.in, deref(got), deref(tt.want))
			}
		})
	}
}

// Truncation must not split a multi-byte character, which would produce
// invalid UTF-8 that PostgreSQL rejects.
func TestNormalizeUserAgentTruncatesOnRuneBoundary(t *testing.T) {
	for _, prefix := range []string{"", "a", "ab", "abc"} {
		got := normalizeUserAgent(prefix + strings.Repeat("日", 200))
		if got == nil || len(*got) > maxUserAgentBytes || !utf8.ValidString(*got) {
			t.Fatalf("prefix %q: got %v (%d bytes)", prefix, deref(got), len(deref(got)))
		}
		if len(*got) < maxUserAgentBytes-utf8.UTFMax {
			t.Errorf("prefix %q: truncated to %d bytes, more than one rune short of the limit", prefix, len(*got))
		}
	}
}

func ptr(s string) *string { return &s }

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

// ---- Store ----

// seedAccount creates an account with the given stored password hash,
// verified or not, and returns its id.
func seedAccount(t *testing.T, pool *pgxpool.Pool, addr, passwordHash string, verified bool) string {
	t.Helper()
	tok := NewToken("")
	created, err := createUserWithVerificationToken(context.Background(), pool, addr, passwordHash, tok.Hash, verificationTokenTTL)
	if err != nil || !created {
		t.Fatalf("seed account: created=%v err=%v", created, err)
	}
	if verified {
		if ok, err := consumeVerificationToken(context.Background(), pool, tok.Hash); err != nil || !ok {
			t.Fatalf("verify seeded account: ok=%v err=%v", ok, err)
		}
	}
	var id string
	if err := pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, addr).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// seedGoogleAccount creates a verified account without a password, linked to
// a Google identity, as Google sign-in does (decision 020), and returns its id.
func seedGoogleAccount(t *testing.T, pool *pgxpool.Pool, addr, subject string) string {
	t.Helper()
	ctx := context.Background()
	var id string
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`INSERT INTO users (email, password_hash, email_verified_at) VALUES ($1, NULL, now()) RETURNING id`,
			addr).Scan(&id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO user_identities (user_id, provider, subject) VALUES ($1, 'google', $2)`, id, subject)
		return err
	})
	if err != nil {
		t.Fatalf("seed google account: %v", err)
	}
	return id
}

type sessionRow struct {
	id, userID                            string
	accessHash, refreshHash, previousHash []byte
	accessExpiresAt, refreshExpiresAt     time.Time
	expiresAt, createdAt, lastUsedAt      time.Time
	revokedAt                             *time.Time
	userAgent                             *string
}

func loadSessions(t *testing.T, pool *pgxpool.Pool) []sessionRow {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT id, user_id, access_token_hash, refresh_token_hash, previous_refresh_token_hash,
		        access_expires_at, refresh_expires_at, expires_at, created_at, last_used_at, revoked_at, user_agent
		 FROM sessions ORDER BY created_at, id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []sessionRow
	for rows.Next() {
		var r sessionRow
		if err := rows.Scan(&r.id, &r.userID, &r.accessHash, &r.refreshHash, &r.previousHash,
			&r.accessExpiresAt, &r.refreshExpiresAt, &r.expiresAt, &r.createdAt, &r.lastUsedAt, &r.revokedAt, &r.userAgent); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func sessionCount(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	return countRows(t, pool, `SELECT count(*) FROM sessions`)
}

// requireFreshSession checks every column login initializes (decision 013).
func requireFreshSession(t *testing.T, r sessionRow, userID string, access, refresh Token, ua *string) {
	t.Helper()
	if r.userID != userID {
		t.Errorf("user_id = %s, want %s", r.userID, userID)
	}
	if string(r.accessHash) != string(access.Hash) || string(r.refreshHash) != string(refresh.Hash) {
		t.Error("stored hashes are not the SHA-256 of the issued tokens")
	}
	if r.previousHash != nil || r.revokedAt != nil {
		t.Errorf("previous_refresh_token_hash = %x, revoked_at = %v; want both NULL", r.previousHash, r.revokedAt)
	}
	for name, got := range map[string]time.Duration{
		"access":  r.accessExpiresAt.Sub(r.createdAt),
		"refresh": r.refreshExpiresAt.Sub(r.createdAt),
		"session": r.expiresAt.Sub(r.createdAt),
	} {
		want := map[string]time.Duration{"access": accessTokenTTL, "refresh": refreshTokenTTL, "session": sessionMaxLifetime}[name]
		if got != want {
			t.Errorf("%s lifetime = %v, want exactly %v", name, got, want)
		}
	}
	if !r.lastUsedAt.Equal(r.createdAt) {
		t.Errorf("last_used_at = %v, want created_at %v", r.lastUsedAt, r.createdAt)
	}
	if deref(r.userAgent) != deref(ua) {
		t.Errorf("user_agent = %q, want %q", deref(r.userAgent), deref(ua))
	}
}

func TestFindUserByEmail(t *testing.T) {
	pool := testutil.DB(t)
	ctx := context.Background()
	verifiedID := seedAccount(t, pool, "ana@example.com", "hash-a", true)
	unverifiedID := seedAccount(t, pool, "bob@example.com", "hash-b", false)

	u, found, err := findUserByEmail(ctx, pool, "ana@example.com")
	if err != nil || !found || u != (loginUser{id: verifiedID, passwordHash: "hash-a", verified: true}) {
		t.Errorf("verified: %+v found=%v err=%v", u, found, err)
	}
	u, found, err = findUserByEmail(ctx, pool, "bob@example.com")
	if err != nil || !found || u != (loginUser{id: unverifiedID, passwordHash: "hash-b", verified: false}) {
		t.Errorf("unverified: %+v found=%v err=%v", u, found, err)
	}
	googleID := seedGoogleAccount(t, pool, "gina@example.com", "1001")
	u, found, err = findUserByEmail(ctx, pool, "gina@example.com")
	if err != nil || !found || u != (loginUser{id: googleID, passwordHash: "", verified: true}) {
		t.Errorf("passwordless: %+v found=%v err=%v", u, found, err)
	}
	if _, found, err = findUserByEmail(ctx, pool, "nobody@example.com"); err != nil || found {
		t.Errorf("unknown: found=%v err=%v", found, err)
	}
}

func TestCreateSession(t *testing.T) {
	pool := testutil.DB(t)
	userID := seedAccount(t, pool, "ana@example.com", "hash-a", true)
	access, refresh := NewToken(AccessTokenPrefix), NewToken(RefreshTokenPrefix)
	ua := ptr("VocaTogether/1.0")

	id, err := createSession(context.Background(), pool, userID, "hash-a", access.Hash, refresh.Hash, ua)
	if err != nil {
		t.Fatal(err)
	}
	sessions := loadSessions(t, pool)
	if len(sessions) != 1 || sessions[0].id != id {
		t.Fatalf("sessions = %d (id %s), want the one created", len(sessions), id)
	}
	requireFreshSession(t, sessions[0], userID, access, refresh, ua)
}

func TestCreateSessionWithoutUserAgent(t *testing.T) {
	pool := testutil.DB(t)
	userID := seedAccount(t, pool, "ana@example.com", "hash-a", true)
	access, refresh := NewToken(AccessTokenPrefix), NewToken(RefreshTokenPrefix)
	if _, err := createSession(context.Background(), pool, userID, "hash-a", access.Hash, refresh.Hash, nil); err != nil {
		t.Fatal(err)
	}
	if ua := loadSessions(t, pool)[0].userAgent; ua != nil {
		t.Errorf("user_agent = %q, want NULL", *ua)
	}
}

// The hash login verified against must still be current when the session is
// written; otherwise the password changed in between and the login fails.
func TestCreateSessionRejectsChangedPassword(t *testing.T) {
	pool := testutil.DB(t)
	userID := seedAccount(t, pool, "ana@example.com", "hash-new", true)
	_, err := createSession(context.Background(), pool, userID, "hash-old",
		NewToken(AccessTokenPrefix).Hash, NewToken(RefreshTokenPrefix).Hash, nil)
	if !errors.Is(err, errPasswordChanged) {
		t.Fatalf("err = %v, want errPasswordChanged", err)
	}
	if n := sessionCount(t, pool); n != 0 {
		t.Errorf("sessions = %d, want 0", n)
	}
}

// A password change (such as a reset) that holds the user row while
// login is between verifying and inserting must win: once it commits, the
// login's re-check sees the new hash and creates nothing. Otherwise reset's
// "revoke all sessions" could miss a session created with the old password.
func TestCreateSessionWaitingForPasswordChangeCreatesNothing(t *testing.T) {
	pool := testutil.DB(t)
	ctx := context.Background()
	userID := seedAccount(t, pool, "ana@example.com", "hash-old", true)

	tx := lockUser(t, pool, "ana@example.com")
	if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = 'hash-new' WHERE id = $1`, userID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := createSession(ctx, pool, userID, "hash-old", NewToken(AccessTokenPrefix).Hash, NewToken(RefreshTokenPrefix).Hash, nil)
		done <- err
	}()
	waitForLockWaiters(t, pool, 1)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	if err := <-done; !errors.Is(err, errPasswordChanged) {
		t.Fatalf("err = %v, want errPasswordChanged", err)
	}
	if n := sessionCount(t, pool); n != 0 {
		t.Errorf("sessions = %d, want 0", n)
	}
}

// If the transaction holding the user row doesn't change the password, the
// waiting login proceeds normally.
func TestCreateSessionWaitingForUnrelatedLockSucceeds(t *testing.T) {
	pool := testutil.DB(t)
	ctx := context.Background()
	userID := seedAccount(t, pool, "ana@example.com", "hash-a", true)

	tx := lockUser(t, pool, "ana@example.com")
	done := make(chan error, 1)
	go func() {
		_, err := createSession(ctx, pool, userID, "hash-a", NewToken(AccessTokenPrefix).Hash, NewToken(RefreshTokenPrefix).Hash, nil)
		done <- err
	}()
	waitForLockWaiters(t, pool, 1)
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if n := sessionCount(t, pool); n != 1 {
		t.Errorf("sessions = %d, want 1", n)
	}
}

// Logins share the user row lock (FOR SHARE), so a login in progress never
// blocks another login of the same account.
func TestCreateSessionDoesNotBlockConcurrentLogins(t *testing.T) {
	pool := testutil.DB(t)
	ctx := context.Background()
	userID := seedAccount(t, pool, "ana@example.com", "hash-a", true)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR SHARE`, userID); err != nil {
		t.Fatal(err)
	}

	short, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, err := createSession(short, pool, userID, "hash-a", NewToken(AccessTokenPrefix).Hash, NewToken(RefreshTokenPrefix).Hash, nil); err != nil {
		t.Fatalf("login blocked by a concurrent login's share lock: %v", err)
	}
}

func TestCreateSessionRollsBackOnInsertFailure(t *testing.T) {
	pool := testutil.DB(t)
	userID := seedAccount(t, pool, "ana@example.com", "hash-a", true)
	injectFailure(t, pool, "BEFORE INSERT ON sessions")

	id, err := createSession(context.Background(), pool, userID, "hash-a",
		NewToken(AccessTokenPrefix).Hash, NewToken(RefreshTokenPrefix).Hash, nil)
	if err == nil || errors.Is(err, errPasswordChanged) || id != "" {
		t.Fatalf("id = %q, err = %v; want an insert error", id, err)
	}
	if n := sessionCount(t, pool); n != 0 {
		t.Errorf("sessions = %d, want 0", n)
	}
}

func TestCreateSessionHonorsCancelledContext(t *testing.T) {
	pool := testutil.DB(t)
	userID := seedAccount(t, pool, "ana@example.com", "hash-a", true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := createSession(ctx, pool, userID, "hash-a", NewToken(AccessTokenPrefix).Hash, NewToken(RefreshTokenPrefix).Hash, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if n := sessionCount(t, pool); n != 0 {
		t.Errorf("sessions = %d, want 0", n)
	}
}

func TestUpdatePasswordHashIsCompareAndSwap(t *testing.T) {
	pool := testutil.DB(t)
	ctx := context.Background()
	userID := seedAccount(t, pool, "ana@example.com", "hash-old", true)
	before := loadUser(t, pool, "ana@example.com")

	if updated, err := updatePasswordHash(ctx, pool, userID, "hash-stale", "hash-x"); err != nil || updated {
		t.Fatalf("stale CAS: updated=%v err=%v; want no update", updated, err)
	}
	if got := loadUser(t, pool, "ana@example.com").passwordHash; got != "hash-old" {
		t.Fatalf("stale CAS overwrote the hash: %q", got)
	}

	if updated, err := updatePasswordHash(ctx, pool, userID, "hash-old", "hash-new"); err != nil || !updated {
		t.Fatalf("CAS: updated=%v err=%v", updated, err)
	}
	after := loadUser(t, pool, "ana@example.com")
	if after.passwordHash != "hash-new" || !after.updatedAt.After(before.updatedAt) {
		t.Errorf("after CAS: hash %q, updated_at %v (before %v)", after.passwordHash, after.updatedAt, before.updatedAt)
	}
}

// ---- Service.Login ----

const testUserAgent = "VocaTogether/1.0 (Android 15; Pixel 9)"

// loginFixture is a service with a verified account for ana@example.com
// whose password is testPassword.
type loginFixture struct {
	testService
	userID string
}

func newLoginFixture(t *testing.T) loginFixture {
	t.Helper()
	s := newTestService(t, &email.Recorder{})
	id := seedAccount(t, s.pool, "ana@example.com", HashPassword(testPassword), true)
	return loginFixture{testService: s, userID: id}
}

// requireLoginLogsClean checks the service log for every secret login
// handles: password, email, user agent, and each issued token with its hash.
func requireLoginLogsClean(t *testing.T, s testService, results ...Credentials) {
	t.Helper()
	s.Wait()
	secrets := []string{testPassword, "ana@example.com", "Ana@Example.com", testUserAgent}
	for _, r := range results {
		secrets = append(secrets, tokenSecrets(r.AccessToken.Raw)...)
		secrets = append(secrets, tokenSecrets(r.RefreshToken.Raw)...)
	}
	requireNoSecrets(t, "log", s.logs.String(), secrets)
}

func requireErrorIs(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func requireNoSessions(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if n := sessionCount(t, pool); n != 0 {
		t.Errorf("sessions = %d, want 0", n)
	}
}

func TestLogin(t *testing.T) {
	f := newLoginFixture(t)
	res, err := f.Login(context.Background(), "  Ana@Example.com ", testPassword, testUserAgent)
	if err != nil {
		t.Fatal(err)
	}

	if !wellFormedToken(res.AccessToken.Raw, AccessTokenPrefix) || !wellFormedToken(res.RefreshToken.Raw, RefreshTokenPrefix) {
		t.Errorf("tokens not well formed: %q %q", res.AccessToken.Raw, res.RefreshToken.Raw)
	}
	if res.ExpiresIn != 15*time.Minute {
		t.Errorf("ExpiresIn = %v, want 15m", res.ExpiresIn)
	}
	sessions := loadSessions(t, f.pool)
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}
	requireFreshSession(t, sessions[0], f.userID, res.AccessToken, res.RefreshToken, ptr(testUserAgent))

	// Only hashes are stored.
	dump := dumpTables(t, f.pool)
	requireNoSecrets(t, "database", dump, []string{res.AccessToken.Raw, res.RefreshToken.Raw, testPassword})

	requireLoginLogsClean(t, f.testService, res)
	logs := f.logs.String()
	if !strings.Contains(logs, "auth: login succeeded") || !strings.Contains(logs, f.userID) ||
		!strings.Contains(logs, sessions[0].id) {
		t.Errorf("success not logged with user and session ids: %s", logs)
	}
}

// Every login gets its own session and fresh credentials; earlier sessions
// stay valid (one session per device).
func TestLoginCreatesFreshSessionEachTime(t *testing.T) {
	f := newLoginFixture(t)
	first, err := f.Login(context.Background(), "ana@example.com", testPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.Login(context.Background(), "ana@example.com", testPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	if first.AccessToken.Raw == second.AccessToken.Raw || first.RefreshToken.Raw == second.RefreshToken.Raw {
		t.Error("second login reused credentials")
	}
	sessions := loadSessions(t, f.pool)
	if len(sessions) != 2 || sessions[0].revokedAt != nil || sessions[1].revokedAt != nil {
		t.Fatalf("want two live sessions, got %+v", sessions)
	}
	if sessions[0].userAgent != nil {
		t.Errorf("empty user agent stored as %q, want NULL", *sessions[0].userAgent)
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	f := newLoginFixture(t)
	_, err := f.Login(context.Background(), "ana@example.com", "wrong-password-123", testUserAgent)
	requireErrorIs(t, err, ErrInvalidCredentials)
	requireNoSessions(t, f.pool)
	requireLoginLogsClean(t, f.testService)
	if logs := f.logs.String(); !strings.Contains(logs, "reason=invalid_credentials") || !strings.Contains(logs, f.userID) {
		t.Errorf("failure not logged with reason and user id: %s", logs)
	}
}

func TestLoginRejectsUnknownEmail(t *testing.T) {
	f := newLoginFixture(t)
	_, err := f.Login(context.Background(), "nobody@example.com", testPassword, testUserAgent)
	requireErrorIs(t, err, ErrInvalidCredentials)
	requireNoSessions(t, f.pool)
	requireLoginLogsClean(t, f.testService)
	logs := f.logs.String()
	if !strings.Contains(logs, "reason=invalid_credentials") || strings.Contains(logs, "user_id") ||
		strings.Contains(logs, "nobody@example.com") {
		t.Errorf("unknown-email failure logged wrongly: %s", logs)
	}
}

// An account created with Google has no password: any password gets the
// same answer as a wrong one, after the same argon2 work (dummy hash).
func TestLoginPasswordlessAccount(t *testing.T) {
	f := newLoginFixture(t)
	googleID := seedGoogleAccount(t, f.pool, "gina@example.com", "1001")

	_, passwordless := f.Login(context.Background(), "gina@example.com", testPassword, testUserAgent)
	_, wrong := f.Login(context.Background(), "ana@example.com", "wrong-password-123", testUserAgent)
	if passwordless != wrong || passwordless != ErrInvalidCredentials {
		t.Fatalf("passwordless = %v, wrong = %v; want the identical ErrInvalidCredentials", passwordless, wrong)
	}
	requireNoSessions(t, f.pool)
	requireLoginLogsClean(t, f.testService)
	if logs := f.logs.String(); !strings.Contains(logs, "reason=no_password") || !strings.Contains(logs, googleID) ||
		strings.Contains(logs, "gina@example.com") {
		t.Errorf("passwordless failure logged wrongly: %s", logs)
	}
}

// Unknown email and wrong password return the very same error value, so no
// caller can tell them apart.
func TestLoginUnknownEmailAndWrongPasswordAreIdentical(t *testing.T) {
	f := newLoginFixture(t)
	_, unknown := f.Login(context.Background(), "nobody@example.com", testPassword, "")
	_, wrong := f.Login(context.Background(), "ana@example.com", "wrong-password-123", "")
	if unknown != wrong || unknown != ErrInvalidCredentials {
		t.Fatalf("unknown = %v, wrong = %v; want the identical ErrInvalidCredentials", unknown, wrong)
	}
}

func TestLoginUnverifiedAccount(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	seedAccount(t, s.pool, "ana@example.com", HashPassword(testPassword), false)

	_, err := s.Login(context.Background(), "ana@example.com", testPassword, testUserAgent)
	requireErrorIs(t, err, ErrEmailNotVerified)
	// Unverified is revealed only with the right password.
	_, err = s.Login(context.Background(), "ana@example.com", "wrong-password-123", testUserAgent)
	requireErrorIs(t, err, ErrInvalidCredentials)

	requireNoSessions(t, s.pool)
	requireLoginLogsClean(t, s)
	if !strings.Contains(s.logs.String(), "reason=email_not_verified") {
		t.Errorf("unverified failure not logged: %s", s.logs.String())
	}
}

func TestLoginMalformedStoredHash(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	id := seedAccount(t, s.pool, "ana@example.com", "not-a-phc-hash", true)

	_, err := s.Login(context.Background(), "ana@example.com", testPassword, "")
	requireErrorIs(t, err, ErrInvalidCredentials)
	requireNoSessions(t, s.pool)
	requireLoginLogsClean(t, s)
	if logs := s.logs.String(); !strings.Contains(logs, "level=ERROR") || !strings.Contains(logs, "malformed") ||
		!strings.Contains(logs, id) {
		t.Errorf("malformed hash not logged as an error with the user id: %s", logs)
	}
}

func TestLoginValidation(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	s.pool.Close() // validation happens before any database access

	tests := []struct {
		email, password string
		want            []*FieldError
	}{
		{"not-an-email", testPassword, []*FieldError{ErrEmailInvalid}},
		{"", testPassword, []*FieldError{ErrEmailInvalid}},
		{"ana@example.com", "", []*FieldError{ErrPasswordRequired}},
		{"", "", []*FieldError{ErrEmailInvalid, ErrPasswordRequired}},
	}
	for _, tt := range tests {
		_, err := s.Login(context.Background(), tt.email, tt.password, "")
		var verr *ValidationError
		if !errors.As(err, &verr) || !slices.Equal(verr.Fields, tt.want) {
			t.Errorf("Login(%q, %q) = %v, want fields %v", tt.email, tt.password, err, tt.want)
		}
	}
}

// Login doesn't apply the registration password policy: an account's
// password is whatever it was allowed to be when set.
func TestLoginDoesNotRevalidatePasswordPolicy(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	seedAccount(t, s.pool, "ana@example.com", HashPassword("short"), true)
	if _, err := s.Login(context.Background(), "ana@example.com", "short", ""); err != nil {
		t.Fatalf("err = %v", err)
	}
}

// Every credential check, including for an unknown email or a malformed
// stored hash, goes through the argon2 limiter: with the only slot taken,
// each waits until its deadline. This proves the unknown-email path does
// real password work (dummy hash) instead of returning early.
func TestLoginAlwaysDoesPasswordWork(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	seedAccount(t, s.pool, "ana@example.com", HashPassword(testPassword), true)
	seedAccount(t, s.pool, "bad@example.com", "not-a-phc-hash", true)
	seedGoogleAccount(t, s.pool, "gina@example.com", "1001")
	s.hashSlots = make(chan struct{}, 1)
	s.hashSlots <- struct{}{}

	for _, addr := range []string{"ana@example.com", "nobody@example.com", "bad@example.com", "gina@example.com"} {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		_, err := s.Login(ctx, addr, "wrong-password-123", "")
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("%s: err = %v, want DeadlineExceeded while waiting for a hash slot", addr, err)
		}
	}
	requireNoSessions(t, s.pool)
}

// A malformed stored hash is rejected by parsing before any argon2 work; the
// service must still spend the time of a real verification.
func TestVerifyPasswordMalformedHashCostsAFullVerification(t *testing.T) {
	s := NewService(nil, nil, nil, slog.New(slog.DiscardHandler), AccountLimits{})
	timeIt := func(f func()) time.Duration {
		best := time.Duration(1<<63 - 1)
		for range 3 {
			start := time.Now()
			f()
			best = min(best, time.Since(start))
		}
		return best
	}
	full := timeIt(func() { _, _, _ = s.verifyPassword(context.Background(), s.dummyHash, testPassword) })
	malformed := timeIt(func() {
		if _, _, err := s.verifyPassword(context.Background(), "not-a-phc-hash", testPassword); !errors.Is(err, ErrMalformedHash) {
			t.Errorf("err = %v, want ErrMalformedHash", err)
		}
	})
	if malformed < full/2 {
		t.Errorf("malformed hash took %v, a real verification %v", malformed, full)
	}
}

func TestLoginRehashesOutdatedHash(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	weak := argonParams{memoryKiB: 8 * 1024, iterations: 1, parallelism: 1, saltLen: 16, keyLen: 32}
	seedAccount(t, s.pool, "ana@example.com", hashPasswordWith(weak, testPassword), true)

	if _, err := s.Login(context.Background(), "ana@example.com", testPassword, ""); err != nil {
		t.Fatal(err)
	}
	stored := loadUser(t, s.pool, "ana@example.com").passwordHash
	if p, _, _, err := parseHash(stored); err != nil || p != defaultParams {
		t.Fatalf("stored params = %+v (err %v), want defaultParams", p, err)
	}
	if ok, needsRehash, err := VerifyPassword(stored, testPassword); !ok || needsRehash || err != nil {
		t.Fatalf("rehashed password doesn't verify: ok=%v needsRehash=%v err=%v", ok, needsRehash, err)
	}
	// The new hash works for the next login.
	if _, err := s.Login(context.Background(), "ana@example.com", testPassword, ""); err != nil {
		t.Fatal(err)
	}
	requireLoginLogsClean(t, s)
}

func TestLoginCurrentHashIsNotRewritten(t *testing.T) {
	f := newLoginFixture(t)
	before := loadUser(t, f.pool, "ana@example.com")
	if _, err := f.Login(context.Background(), "ana@example.com", testPassword, ""); err != nil {
		t.Fatal(err)
	}
	after := loadUser(t, f.pool, "ana@example.com")
	if after.passwordHash != before.passwordHash || !after.updatedAt.Equal(before.updatedAt) {
		t.Error("login rewrote a current password hash")
	}
}

// Rehashing is best effort: if it fails, the login (already committed) still
// succeeds and the old hash stays usable.
func TestLoginSucceedsWhenRehashFails(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	weak := argonParams{memoryKiB: 8 * 1024, iterations: 1, parallelism: 1, saltLen: 16, keyLen: 32}
	oldHash := hashPasswordWith(weak, testPassword)
	seedAccount(t, s.pool, "ana@example.com", oldHash, true)
	injectFailure(t, s.pool, "BEFORE UPDATE ON users")

	res, err := s.Login(context.Background(), "ana@example.com", testPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	if n := sessionCount(t, s.pool); n != 1 {
		t.Errorf("sessions = %d, want 1", n)
	}
	if got := loadUser(t, s.pool, "ana@example.com").passwordHash; got != oldHash {
		t.Error("hash changed despite the failed update")
	}
	requireLoginLogsClean(t, s, res)
	if logs := s.logs.String(); !strings.Contains(logs, "level=WARN") || !strings.Contains(logs, "rehash failed") {
		t.Errorf("rehash failure not logged: %s", logs)
	}
}

// End to end through the service: a password change that holds the user row
// while login is verifying makes the login fail once it commits.
func TestLoginFailsWhenPasswordChangesConcurrently(t *testing.T) {
	f := newLoginFixture(t)
	ctx := context.Background()
	tx := lockUser(t, f.pool, "ana@example.com")
	if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`,
		f.userID, HashPassword("a-brand-new-password")); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		// Reads the committed (old) hash, which the password matches, then
		// waits for the user row in createSession.
		_, err := f.Login(ctx, "ana@example.com", testPassword, "")
		done <- err
	}()
	waitForLockWaiters(t, f.pool, 1)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	requireErrorIs(t, <-done, ErrInvalidCredentials)
	requireNoSessions(t, f.pool)
}

func TestLoginInsertFailureReturnsNoCredentials(t *testing.T) {
	f := newLoginFixture(t)
	injectFailure(t, f.pool, "BEFORE INSERT ON sessions")

	res, err := f.Login(context.Background(), "ana@example.com", testPassword, testUserAgent)
	var verr *ValidationError
	if err == nil || errors.Is(err, ErrInvalidCredentials) || errors.As(err, &verr) {
		t.Fatalf("err = %v, want an internal error", err)
	}
	if res.AccessToken.Raw != "" || res.RefreshToken.Raw != "" {
		t.Error("credentials returned although the session wasn't stored")
	}
	requireNoSessions(t, f.pool)
	requireLoginLogsClean(t, f.testService)
}

func TestLoginHonorsCancelledContext(t *testing.T) {
	f := newLoginFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := f.Login(ctx, "ana@example.com", testPassword, "")
	requireErrorIs(t, err, context.Canceled)
	if res.AccessToken.Raw != "" || res.RefreshToken.Raw != "" {
		t.Error("credentials returned for a cancelled login")
	}
	requireNoSessions(t, f.pool)
}

func TestLoginDatabaseFailure(t *testing.T) {
	f := newLoginFixture(t)
	f.pool.Close()
	_, err := f.Login(context.Background(), "ana@example.com", testPassword, testUserAgent)
	var verr *ValidationError
	if err == nil || errors.Is(err, ErrInvalidCredentials) || errors.As(err, &verr) {
		t.Fatalf("err = %v, want a database error", err)
	}
	requireNoSecrets(t, "error", err.Error(), []string{"ana@example.com", testPassword, testUserAgent})
}

func TestLoginConcurrentSameAccount(t *testing.T) {
	f := newLoginFixture(t)
	const n = 20
	results := make([]Credentials, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			res, err := f.Login(context.Background(), "ana@example.com", testPassword, "")
			if err != nil {
				t.Errorf("login %d: %v", i, err)
			}
			results[i] = res
		})
	}
	wg.Wait()

	sessions := loadSessions(t, f.pool)
	if len(sessions) != n {
		t.Fatalf("sessions = %d, want %d", len(sessions), n)
	}
	seen := map[string]bool{}
	for _, r := range results {
		for _, raw := range []string{r.AccessToken.Raw, r.RefreshToken.Raw} {
			if seen[raw] {
				t.Fatal("two logins got the same token")
			}
			seen[raw] = true
		}
	}
	requireLoginLogsClean(t, f.testService, results...)
}

func TestCredentialsAreRedacted(t *testing.T) {
	res := Credentials{AccessToken: NewToken(AccessTokenPrefix), RefreshToken: NewToken(RefreshTokenPrefix), ExpiresIn: accessTokenTTL}
	var jsonLog, textLog bytes.Buffer
	slog.New(slog.NewJSONHandler(&jsonLog, nil)).Info("x", "res", res)
	slog.New(slog.NewTextHandler(&textLog, nil)).Info("x", "res", res)
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	outputs := map[string]string{
		"%v": fmt.Sprintf("%v", res), "%+v": fmt.Sprintf("%+v", res), "%#v": fmt.Sprintf("%#v", res),
		"json": string(b), "slog JSON": jsonLog.String(), "slog text": textLog.String(),
	}
	secrets := append(tokenSecrets(res.AccessToken.Raw), tokenSecrets(res.RefreshToken.Raw)...)
	for name, s := range outputs {
		requireNoSecrets(t, name, s, secrets)
	}
}
