package auth

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// refreshFixture is a loginFixture with one logged-in session.
type refreshFixture struct {
	loginFixture
	login Credentials
}

func newRefreshFixture(t *testing.T) refreshFixture {
	t.Helper()
	f := newLoginFixture(t)
	res, err := f.Login(context.Background(), "ana@example.com", testPassword, testUserAgent)
	if err != nil {
		t.Fatal(err)
	}
	return refreshFixture{loginFixture: f, login: res}
}

// session returns the only session row.
func (f refreshFixture) session(t *testing.T) sessionRow {
	t.Helper()
	sessions := loadSessions(t, f.pool)
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}
	return sessions[0]
}

// setSessionExpiry moves the session's absolute expiry to now()+d (d may be
// negative), pulling the token expiries down with it to keep the
// sessions_expiry_order CHECK satisfied.
func setSessionExpiry(t *testing.T, pool *pgxpool.Pool, id string, d time.Duration) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE sessions SET expires_at = now() + make_interval(secs => $2),
		     access_expires_at  = LEAST(access_expires_at,  now() + make_interval(secs => $2)),
		     refresh_expires_at = LEAST(refresh_expires_at, now() + make_interval(secs => $2))
		 WHERE id = $1`, id, d.Seconds()); err != nil {
		t.Fatal(err)
	}
}

// lockSession starts a transaction holding the session's row lock, standing
// in for a concurrent refresh that got there first.
func lockSession(t *testing.T, pool *pgxpool.Pool, id string) pgx.Tx {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	if _, err := tx.Exec(ctx, `SELECT id FROM sessions WHERE id = $1 FOR UPDATE`, id); err != nil {
		t.Fatal(err)
	}
	return tx
}

func requireInvalidRefresh(t *testing.T, res Credentials, err error) {
	t.Helper()
	if !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("err = %v, want ErrInvalidRefreshToken", err)
	}
	if res.AccessToken.Raw != "" || res.RefreshToken.Raw != "" {
		t.Error("credentials returned with an error")
	}
}

func requireUnchanged(t *testing.T, before, after sessionRow) {
	t.Helper()
	if !reflect.DeepEqual(before, after) {
		t.Errorf("session changed:\nbefore %+v\nafter  %+v", before, after)
	}
}

// requireRefreshLogsClean checks the service log for every token issued in
// the test and each one's hash.
func requireRefreshLogsClean(t *testing.T, s testService, creds ...Credentials) {
	t.Helper()
	var secrets []string
	for _, c := range creds {
		if c.AccessToken.Raw == "" { // a failed refresh issued nothing
			continue
		}
		secrets = append(secrets, tokenSecrets(c.AccessToken.Raw)...)
		secrets = append(secrets, tokenSecrets(c.RefreshToken.Raw)...)
	}
	requireNoSecrets(t, "log", s.logs.String(), secrets)
}

func TestRefresh(t *testing.T) {
	f := newRefreshFixture(t)
	before := f.session(t)

	res, err := f.Refresh(context.Background(), f.login.RefreshToken.Raw)
	if err != nil {
		t.Fatal(err)
	}
	if !wellFormedToken(res.AccessToken.Raw, AccessTokenPrefix) || !wellFormedToken(res.RefreshToken.Raw, RefreshTokenPrefix) {
		t.Errorf("tokens not well formed")
	}
	if res.AccessToken.Raw == f.login.AccessToken.Raw || res.RefreshToken.Raw == f.login.RefreshToken.Raw {
		t.Error("refresh returned an old token")
	}
	if res.ExpiresIn != accessTokenTTL {
		t.Errorf("ExpiresIn = %v, want %v", res.ExpiresIn, accessTokenTTL)
	}

	after := f.session(t)
	if string(after.accessHash) != string(res.AccessToken.Hash) || string(after.refreshHash) != string(res.RefreshToken.Hash) {
		t.Error("stored hashes are not those of the new tokens")
	}
	if string(after.previousHash) != string(f.login.RefreshToken.Hash) {
		t.Error("previous_refresh_token_hash is not the rotated-out token's hash")
	}
	if !after.lastUsedAt.After(before.lastUsedAt) {
		t.Errorf("last_used_at = %v, not after %v", after.lastUsedAt, before.lastUsedAt)
	}
	if got := after.accessExpiresAt.Sub(after.lastUsedAt); got != accessTokenTTL {
		t.Errorf("access lifetime = %v, want %v", got, accessTokenTTL)
	}
	if got := after.refreshExpiresAt.Sub(after.lastUsedAt); got != refreshTokenTTL {
		t.Errorf("refresh lifetime = %v, want %v (slid forward)", got, refreshTokenTTL)
	}
	if !after.expiresAt.Equal(before.expiresAt) || !after.createdAt.Equal(before.createdAt) ||
		deref(after.userAgent) != deref(before.userAgent) || after.revokedAt != nil || after.id != before.id {
		t.Errorf("unrelated columns changed:\nbefore %+v\nafter  %+v", before, after)
	}

	requireNoSecrets(t, "database", dumpTables(t, f.pool), []string{res.AccessToken.Raw, res.RefreshToken.Raw})
	f.Wait()
	requireRefreshLogsClean(t, f.testService, f.login, res)
	if logs := f.logs.String(); !strings.Contains(logs, "auth: refresh succeeded") || !strings.Contains(logs, after.id) {
		t.Errorf("success not logged with the session id: %s", logs)
	}
}

// Each refresh returns tokens that work for the next one; every earlier
// refresh token is dead afterwards.
func TestRefreshChain(t *testing.T) {
	f := newRefreshFixture(t)
	cur := f.login
	for i := range 3 {
		next, err := f.Refresh(context.Background(), cur.RefreshToken.Raw)
		if err != nil {
			t.Fatalf("refresh %d: %v", i, err)
		}
		cur = next
	}
	if f.session(t).revokedAt != nil {
		t.Fatal("chain revoked the session")
	}
	if string(f.session(t).refreshHash) != string(cur.RefreshToken.Hash) {
		t.Error("session doesn't hold the latest token")
	}
}

// The old access token dies with the rotation (decision 014): the session
// holds only the new access hash.
func TestRefreshReplacesAccessToken(t *testing.T) {
	f := newRefreshFixture(t)
	if _, err := f.Refresh(context.Background(), f.login.RefreshToken.Raw); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, f.pool, `SELECT count(*) FROM sessions WHERE access_token_hash = $1`, f.login.AccessToken.Hash); n != 0 {
		t.Error("old access token hash still stored")
	}
}

func TestRefreshCapsExpiriesAtSessionExpiry(t *testing.T) {
	f := newRefreshFixture(t)
	id := f.session(t).id

	setSessionExpiry(t, f.pool, id, 10*24*time.Hour)
	res, err := f.Refresh(context.Background(), f.login.RefreshToken.Raw)
	if err != nil {
		t.Fatal(err)
	}
	s := f.session(t)
	if !s.refreshExpiresAt.Equal(s.expiresAt) {
		t.Errorf("refresh_expires_at = %v, want capped at expires_at %v", s.refreshExpiresAt, s.expiresAt)
	}
	if res.ExpiresIn != accessTokenTTL {
		t.Errorf("ExpiresIn = %v, want %v (far from the cap)", res.ExpiresIn, accessTokenTTL)
	}

	setSessionExpiry(t, f.pool, id, 5*time.Minute)
	res, err = f.Refresh(context.Background(), res.RefreshToken.Raw)
	if err != nil {
		t.Fatal(err)
	}
	s = f.session(t)
	if !s.accessExpiresAt.Equal(s.expiresAt) || !s.refreshExpiresAt.Equal(s.expiresAt) {
		t.Errorf("expiries %v / %v, want both capped at %v", s.accessExpiresAt, s.refreshExpiresAt, s.expiresAt)
	}
	if res.ExpiresIn < minAccessTokenLifetime || res.ExpiresIn > 5*time.Minute {
		t.Errorf("ExpiresIn = %v, want between %v and 5m", res.ExpiresIn, minAccessTokenLifetime)
	}
}

// Near the absolute expiry, refresh is refused rather than issuing an
// access token that expires on arrival (decision 014).
func TestRefreshMinimumAccessLifetime(t *testing.T) {
	f := newRefreshFixture(t)
	id := f.session(t).id

	setSessionExpiry(t, f.pool, id, 30*time.Second)
	before := f.session(t)
	res, err := f.Refresh(context.Background(), f.login.RefreshToken.Raw)
	requireInvalidRefresh(t, res, err)
	requireUnchanged(t, before, f.session(t))

	setSessionExpiry(t, f.pool, id, 90*time.Second)
	res, err = f.Refresh(context.Background(), f.login.RefreshToken.Raw)
	if err != nil {
		t.Fatal(err)
	}
	if res.ExpiresIn < minAccessTokenLifetime || res.ExpiresIn > 90*time.Second {
		t.Errorf("ExpiresIn = %v, want between %v and 90s", res.ExpiresIn, minAccessTokenLifetime)
	}
	f.Wait()
	if logs := f.logs.String(); !strings.Contains(logs, "reason=session_expired") {
		t.Errorf("refusal not logged as session_expired: %s", logs)
	}
}

// Unusable tokens are rejected and change nothing.
func TestRefreshRejectsUnusableSession(t *testing.T) {
	tests := map[string]struct {
		setup  string // SQL run with the session id as $1
		reason string
	}{
		"refresh expired": {`UPDATE sessions SET refresh_expires_at = now() - interval '1 second',
		                         access_expires_at = now() - interval '1 second' WHERE id = $1`, "refresh_expired"},
		"session expired": {`UPDATE sessions SET expires_at = now() - interval '1 second',
		                         refresh_expires_at = now() - interval '1 second',
		                         access_expires_at = now() - interval '1 second' WHERE id = $1`, "session_expired"},
		"revoked": {`UPDATE sessions SET revoked_at = now() WHERE id = $1`, "revoked"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f := newRefreshFixture(t)
			if _, err := f.pool.Exec(context.Background(), tt.setup, f.session(t).id); err != nil {
				t.Fatal(err)
			}
			before := f.session(t)
			res, err := f.Refresh(context.Background(), f.login.RefreshToken.Raw)
			requireInvalidRefresh(t, res, err)
			requireUnchanged(t, before, f.session(t))
			f.Wait()
			if logs := f.logs.String(); !strings.Contains(logs, "reason="+tt.reason) || !strings.Contains(logs, before.id) {
				t.Errorf("failure not logged with reason %s and session id: %s", tt.reason, logs)
			}
			requireRefreshLogsClean(t, f.testService, f.login)
		})
	}
}

func TestRefreshRejectsUnknownToken(t *testing.T) {
	f := newRefreshFixture(t)
	before := f.session(t)
	res, err := f.Refresh(context.Background(), NewToken(RefreshTokenPrefix).Raw)
	requireInvalidRefresh(t, res, err)
	requireUnchanged(t, before, f.session(t))
}

func TestRefreshRejectsMalformedTokenWithoutDatabase(t *testing.T) {
	f := newRefreshFixture(t)
	f.pool.Close() // any database access would now return an error

	valid := f.login.RefreshToken.Raw
	body := strings.TrimPrefix(valid, RefreshTokenPrefix)
	for _, raw := range []string{
		body,                                // no prefix
		f.login.AccessToken.Raw,             // access token presented as refresh token
		AccessTokenPrefix + body,            // right body, wrong prefix
		valid[:len(valid)-1],                // too short
		valid + "A",                         // too long
		valid + "=",                         // padding
		RefreshTokenPrefix + "+" + body[1:], // not base64url
		strings.ToUpper(valid),              // wrong prefix case
		"ñ",
	} {
		res, err := f.Refresh(context.Background(), raw)
		requireInvalidRefresh(t, res, err)
	}
	// Control: a well-formed token does reach the (closed) database.
	if _, err := f.Refresh(context.Background(), valid); err == nil || errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("well-formed token: err = %v, want a database error", err)
	}
}

func TestRefreshRequiresToken(t *testing.T) {
	f := newRefreshFixture(t)
	_, err := f.Refresh(context.Background(), "")
	var verr *ValidationError
	if !errors.As(err, &verr) || !reflect.DeepEqual(verr.Fields, []*FieldError{ErrRefreshTokenRequired}) {
		t.Fatalf("err = %v, want refresh_token:required", err)
	}
}

// Presenting a rotated-out token revokes the session: neither holder of the
// token pair keeps access (decision 002).
func TestRefreshReuseRevokesSession(t *testing.T) {
	f := newRefreshFixture(t)
	// Another session of the same user and one of another user must survive.
	other, err := f.Login(context.Background(), "ana@example.com", testPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	seedAccount(t, f.pool, "ben@example.com", HashPassword(testPassword), true)
	ben, err := f.Login(context.Background(), "ben@example.com", testPassword, "")
	if err != nil {
		t.Fatal(err)
	}

	rotated, err := f.Refresh(context.Background(), f.login.RefreshToken.Raw)
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.Refresh(context.Background(), f.login.RefreshToken.Raw)
	requireInvalidRefresh(t, res, err)

	var revoked bool
	if err := f.pool.QueryRow(context.Background(),
		`SELECT revoked_at IS NOT NULL FROM sessions WHERE refresh_token_hash = $1`, rotated.RefreshToken.Hash).Scan(&revoked); err != nil {
		t.Fatal(err)
	}
	if !revoked {
		t.Fatal("reuse didn't revoke the session")
	}
	// The legitimate-looking current token is dead too.
	res, err = f.Refresh(context.Background(), rotated.RefreshToken.Raw)
	requireInvalidRefresh(t, res, err)

	if n := countRows(t, f.pool, `SELECT count(*) FROM sessions WHERE revoked_at IS NOT NULL`); n != 1 {
		t.Errorf("revoked sessions = %d, want 1", n)
	}
	for _, c := range []Credentials{other, ben} {
		if _, err := f.Refresh(context.Background(), c.RefreshToken.Raw); err != nil {
			t.Errorf("unrelated session affected: %v", err)
		}
	}

	f.Wait()
	logs := f.logs.String()
	if !strings.Contains(logs, "level=WARN") || !strings.Contains(logs, "reuse detected") {
		t.Errorf("reuse not logged as a warning: %s", logs)
	}
	requireRefreshLogsClean(t, f.testService, f.login, rotated, other, ben)
}

func TestRefreshReuseOnRevokedSessionKeepsRevocationTime(t *testing.T) {
	f := newRefreshFixture(t)
	if _, err := f.Refresh(context.Background(), f.login.RefreshToken.Raw); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(context.Background(), `UPDATE sessions SET revoked_at = now() - interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	before := f.session(t)
	res, err := f.Refresh(context.Background(), f.login.RefreshToken.Raw)
	requireInvalidRefresh(t, res, err)
	requireUnchanged(t, before, f.session(t))
}

// Reuse is detected before expiry, so a replay against an expired session
// still revokes it (and is logged as reuse).
func TestRefreshReuseOnExpiredSessionRevokes(t *testing.T) {
	f := newRefreshFixture(t)
	if _, err := f.Refresh(context.Background(), f.login.RefreshToken.Raw); err != nil {
		t.Fatal(err)
	}
	setSessionExpiry(t, f.pool, f.session(t).id, -time.Hour)
	res, err := f.Refresh(context.Background(), f.login.RefreshToken.Raw)
	requireInvalidRefresh(t, res, err)
	if f.session(t).revokedAt == nil {
		t.Error("reuse on an expired session didn't revoke it")
	}
}

// Only one previous hash is kept (decision 014): a token two rotations old
// is unknown, rejected without revoking.
func TestRefreshTwoGenerationsOldIsUnknown(t *testing.T) {
	f := newRefreshFixture(t)
	t2, err := f.Refresh(context.Background(), f.login.RefreshToken.Raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Refresh(context.Background(), t2.RefreshToken.Raw); err != nil {
		t.Fatal(err)
	}
	before := f.session(t)
	res, err := f.Refresh(context.Background(), f.login.RefreshToken.Raw)
	requireInvalidRefresh(t, res, err)
	requireUnchanged(t, before, f.session(t))
	f.Wait()
	if !strings.Contains(f.logs.String(), "reason=unknown") {
		t.Errorf("not logged as unknown: %s", f.logs.String())
	}
}

// A failure during rotation rolls everything back: no credentials, and the
// presented token still works afterwards.
func TestRefreshRotationFailureChangesNothing(t *testing.T) {
	f := newRefreshFixture(t)
	before := f.session(t)
	remove := injectFailure(t, f.pool, "BEFORE UPDATE ON sessions")

	res, err := f.Refresh(context.Background(), f.login.RefreshToken.Raw)
	if err == nil || errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("err = %v, want an internal error", err)
	}
	if res.AccessToken.Raw != "" || res.RefreshToken.Raw != "" {
		t.Error("credentials returned although the rotation failed")
	}
	requireNoSecrets(t, "error", err.Error(), tokenSecrets(f.login.RefreshToken.Raw))
	requireUnchanged(t, before, f.session(t))

	remove()
	if _, err := f.Refresh(context.Background(), f.login.RefreshToken.Raw); err != nil {
		t.Fatalf("token unusable after a failed rotation: %v", err)
	}
}

// A failure while revoking for reuse also rolls back and returns an internal
// error, not a silent 401 with the session left live.
func TestRefreshReuseRevocationFailure(t *testing.T) {
	f := newRefreshFixture(t)
	if _, err := f.Refresh(context.Background(), f.login.RefreshToken.Raw); err != nil {
		t.Fatal(err)
	}
	injectFailure(t, f.pool, "BEFORE UPDATE ON sessions")
	_, err := f.Refresh(context.Background(), f.login.RefreshToken.Raw)
	if err == nil || errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("err = %v, want an internal error", err)
	}
}

func TestRefreshHonorsCancelledContext(t *testing.T) {
	f := newRefreshFixture(t)
	before := f.session(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := f.Refresh(ctx, f.login.RefreshToken.Raw)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if res.AccessToken.Raw != "" {
		t.Error("credentials returned for a cancelled refresh")
	}
	requireUnchanged(t, before, f.session(t))
}

// Two requests with the same token released at the same moment: exactly one
// gets credentials; the other is reuse and revokes the session (strict
// policy, decision 014), so even the winner's tokens end up dead.
func TestRefreshConcurrentSameTokenSucceedsOnce(t *testing.T) {
	f := newRefreshFixture(t)
	blocker := lockSession(t, f.pool, f.session(t).id)

	type result struct {
		res Credentials
		err error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			res, err := f.Refresh(context.Background(), f.login.RefreshToken.Raw)
			results <- result{res, err}
		}()
	}
	waitForLockWaiters(t, f.pool, 2)
	if err := blocker.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}

	var winner Credentials
	var errs []error
	for range 2 {
		r := <-results
		if r.err == nil {
			winner = r.res
		}
		errs = append(errs, r.err)
	}
	requireOneRefresh(t, errs)
	if f.session(t).revokedAt == nil {
		t.Fatal("losing request didn't revoke the session")
	}
	res, err := f.Refresh(context.Background(), winner.RefreshToken.Raw)
	requireInvalidRefresh(t, res, err)
}

// A refresh waiting on a rotation that rolls back proceeds against the
// original row and succeeds.
func TestRefreshWaitingForRolledBackRotationSucceeds(t *testing.T) {
	f := newRefreshFixture(t)
	ctx := context.Background()
	blocker := lockSession(t, f.pool, f.session(t).id)
	if _, err := blocker.Exec(ctx,
		`UPDATE sessions SET previous_refresh_token_hash = refresh_token_hash, refresh_token_hash = $1`,
		NewToken(RefreshTokenPrefix).Hash); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := f.Refresh(ctx, f.login.RefreshToken.Raw)
		done <- err
	}()
	waitForLockWaiters(t, f.pool, 1)
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("refresh after rollback: %v", err)
	}
	if f.session(t).revokedAt != nil {
		t.Error("session revoked although the competing rotation rolled back")
	}
}

func TestRefreshConcurrentStress(t *testing.T) {
	f := newRefreshFixture(t)
	const n = 20
	start := make(chan struct{})
	errs := make([]error, n)
	results := make([]Credentials, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			<-start
			results[i], errs[i] = f.Refresh(context.Background(), f.login.RefreshToken.Raw)
		})
	}
	close(start)
	wg.Wait()
	requireOneRefresh(t, errs)
	if f.session(t).revokedAt == nil {
		t.Error("concurrent reuse didn't revoke the session")
	}
	f.Wait()
	requireRefreshLogsClean(t, f.testService, append(results, f.login)...)
}

func requireOneRefresh(t *testing.T, errs []error) {
	t.Helper()
	successes := 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case !errors.Is(err, ErrInvalidRefreshToken):
			t.Errorf("unexpected error (deadlock or failure?): %v", err)
		}
	}
	if successes != 1 {
		t.Errorf("%d refreshes succeeded, want exactly 1", successes)
	}
}
