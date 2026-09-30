package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// requireLogoutLogsClean checks the service log for every token issued in
// the test and each one's hash.
func requireLogoutLogsClean(t *testing.T, s testService, creds ...Credentials) {
	t.Helper()
	s.Wait()
	requireRefreshLogsClean(t, s, creds...)
}

func TestLogout(t *testing.T) {
	f := newRefreshFixture(t)
	before := f.session(t)

	if err := f.Logout(context.Background(), f.login.AccessToken.Raw); err != nil {
		t.Fatal(err)
	}

	after := f.session(t)
	if after.revokedAt == nil {
		t.Fatal("session not revoked")
	}
	after.revokedAt = nil
	requireUnchanged(t, before, after)

	// Revocation kills the refresh token too.
	res, err := f.Refresh(context.Background(), f.login.RefreshToken.Raw)
	requireInvalidRefresh(t, res, err)

	requireLogoutLogsClean(t, f.testService, f.login)
	if logs := f.logs.String(); !strings.Contains(logs, "auth: logout succeeded") ||
		!strings.Contains(logs, before.id) || !strings.Contains(logs, f.userID) {
		t.Errorf("success not logged with the ids: %s", logs)
	}
}

func TestLogoutRevokesOnlyCurrentSession(t *testing.T) {
	f := newRefreshFixture(t)
	other, err := f.Login(context.Background(), "ana@example.com", testPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	seedAccount(t, f.pool, "ben@example.com", HashPassword(testPassword), true)
	ben, err := f.Login(context.Background(), "ben@example.com", testPassword, "")
	if err != nil {
		t.Fatal(err)
	}

	if err := f.Logout(context.Background(), f.login.AccessToken.Raw); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, f.pool, `SELECT count(*) FROM sessions WHERE revoked_at IS NOT NULL`); n != 1 {
		t.Errorf("revoked sessions = %d, want 1", n)
	}
	for _, c := range []Credentials{other, ben} {
		if _, err := f.Refresh(context.Background(), c.RefreshToken.Raw); err != nil {
			t.Errorf("unrelated session affected: %v", err)
		}
	}
}

func TestLogoutIsIdempotent(t *testing.T) {
	f := newRefreshFixture(t)
	if err := f.Logout(context.Background(), f.login.AccessToken.Raw); err != nil {
		t.Fatal(err)
	}
	before := f.session(t)
	if err := f.Logout(context.Background(), f.login.AccessToken.Raw); err != nil {
		t.Fatalf("second logout: %v", err)
	}
	requireUnchanged(t, before, f.session(t))
	f.Wait()
	if n := strings.Count(f.logs.String(), "auth: logout succeeded"); n != 1 {
		t.Errorf("logout succeeded logged %d times, want 1", n)
	}
	if !strings.Contains(f.logs.String(), "auth: logout had no effect") {
		t.Errorf("no-op not logged: %s", f.logs.String())
	}
}

// An expired but current access token still revokes its session: logout
// only reduces privilege, and an idle client shouldn't have to refresh first.
func TestLogoutWithExpiredAccessToken(t *testing.T) {
	f := newRefreshFixture(t)
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE sessions SET access_expires_at = now() - interval '1 minute'`); err != nil {
		t.Fatal(err)
	}
	if err := f.Logout(context.Background(), f.login.AccessToken.Raw); err != nil {
		t.Fatal(err)
	}
	if f.session(t).revokedAt == nil {
		t.Error("expired access token didn't revoke its session")
	}
}

func TestLogoutExpiredSession(t *testing.T) {
	f := newRefreshFixture(t)
	setSessionExpiry(t, f.pool, f.session(t).id, -time.Hour)
	if err := f.Logout(context.Background(), f.login.AccessToken.Raw); err != nil {
		t.Fatal(err)
	}
}

func TestLogoutUnknownToken(t *testing.T) {
	f := newRefreshFixture(t)
	before := f.session(t)
	if err := f.Logout(context.Background(), NewToken(AccessTokenPrefix).Raw); err != nil {
		t.Fatal(err)
	}
	requireUnchanged(t, before, f.session(t))
	f.Wait()
	// An unknown token must not be linked to an account in the log.
	for line := range strings.Lines(f.logs.String()) {
		if strings.Contains(line, "auth: logout") &&
			(!strings.Contains(line, "no effect") || strings.Contains(line, "_id=")) {
			t.Errorf("unexpected logout log line: %s", line)
		}
	}
}

// Rotation replaces the access token (decision 014), so logging out with a
// rotated-out one is a no-op: the session stays live (decision 015).
func TestLogoutRotatedOutAccessToken(t *testing.T) {
	f := newRefreshFixture(t)
	rotated, err := f.Refresh(context.Background(), f.login.RefreshToken.Raw)
	if err != nil {
		t.Fatal(err)
	}
	before := f.session(t)
	if err := f.Logout(context.Background(), f.login.AccessToken.Raw); err != nil {
		t.Fatal(err)
	}
	requireUnchanged(t, before, f.session(t))
	if _, err := f.Refresh(context.Background(), rotated.RefreshToken.Raw); err != nil {
		t.Errorf("session unusable after logout with a stale token: %v", err)
	}
}

func TestLogoutAfterReuseRevocationKeepsRevocationTime(t *testing.T) {
	f := newRefreshFixture(t)
	rotated, err := f.Refresh(context.Background(), f.login.RefreshToken.Raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Refresh(context.Background(), f.login.RefreshToken.Raw); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("reuse: err = %v", err)
	}
	before := f.session(t)
	if before.revokedAt == nil {
		t.Fatal("reuse didn't revoke")
	}
	if err := f.Logout(context.Background(), rotated.AccessToken.Raw); err != nil {
		t.Fatal(err)
	}
	requireUnchanged(t, before, f.session(t))
}

func TestLogoutRejectsMalformedTokenWithoutDatabase(t *testing.T) {
	f := newRefreshFixture(t)
	f.pool.Close() // any database access would now return an error

	valid := f.login.AccessToken.Raw
	body := strings.TrimPrefix(valid, AccessTokenPrefix)
	for _, raw := range []string{
		"",
		body,                               // no prefix
		f.login.RefreshToken.Raw,           // refresh token presented as access token
		RefreshTokenPrefix + body,          // right body, wrong prefix
		valid[:len(valid)-1],               // too short
		valid + "A",                        // too long
		valid + "=",                        // padding
		valid + " ",                        // trailing space
		" " + valid,                        // leading space
		AccessTokenPrefix + "+" + body[1:], // not base64url
		strings.ToUpper(valid),             // wrong prefix case
		"ñ",
	} {
		if err := f.Logout(context.Background(), raw); !errors.Is(err, ErrInvalidAccessToken) {
			t.Errorf("Logout(%q): err = %v, want ErrInvalidAccessToken", raw, err)
		}
	}
	// Control: a well-formed token does reach the (closed) database.
	if err := f.Logout(context.Background(), valid); err == nil || errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("well-formed token: err = %v, want a database error", err)
	}
	f.Wait()
	if !strings.Contains(f.logs.String(), "reason=malformed") {
		t.Errorf("malformed token not logged: %s", f.logs.String())
	}
}

func TestLogoutFailureIsInternal(t *testing.T) {
	f := newRefreshFixture(t)
	before := f.session(t)
	injectFailure(t, f.pool, "BEFORE UPDATE ON sessions")

	err := f.Logout(context.Background(), f.login.AccessToken.Raw)
	if err == nil || errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("err = %v, want an internal error", err)
	}
	requireNoSecrets(t, "error", err.Error(), tokenSecrets(f.login.AccessToken.Raw))
	requireUnchanged(t, before, f.session(t))
}

func TestLogoutHonorsCancelledContext(t *testing.T) {
	f := newRefreshFixture(t)
	before := f.session(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.Logout(ctx, f.login.AccessToken.Raw); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	requireUnchanged(t, before, f.session(t))
}

// A logout waiting on a rotation that commits re-checks the row, no longer
// matches the old access token, and changes nothing (decision 015, race 2).
func TestLogoutWaitingForRotation(t *testing.T) {
	f := newRefreshFixture(t)
	ctx := context.Background()
	blocker := lockSession(t, f.pool, f.session(t).id)
	if _, err := blocker.Exec(ctx,
		`UPDATE sessions SET access_token_hash = $1, previous_refresh_token_hash = refresh_token_hash, refresh_token_hash = $2`,
		NewToken(AccessTokenPrefix).Hash, NewToken(RefreshTokenPrefix).Hash); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- f.Logout(ctx, f.login.AccessToken.Raw) }()
	waitForLockWaiters(t, f.pool, 1)
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("logout: %v", err)
	}
	if f.session(t).revokedAt != nil {
		t.Error("stale access token revoked the rotated session")
	}
}

// A logout waiting on a rotation that rolls back proceeds against the
// original row and revokes it (race 3).
func TestLogoutWaitingForRolledBackRotationRevokes(t *testing.T) {
	f := newRefreshFixture(t)
	ctx := context.Background()
	blocker := lockSession(t, f.pool, f.session(t).id)
	if _, err := blocker.Exec(ctx, `UPDATE sessions SET access_token_hash = $1`, NewToken(AccessTokenPrefix).Hash); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- f.Logout(ctx, f.login.AccessToken.Raw) }()
	waitForLockWaiters(t, f.pool, 1)
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("logout: %v", err)
	}
	if f.session(t).revokedAt == nil {
		t.Error("session not revoked after the competing rotation rolled back")
	}
}

// A refresh waiting on a logout that commits sees the revocation and issues
// nothing (race 1).
func TestRefreshWaitingForLogoutFails(t *testing.T) {
	f := newRefreshFixture(t)
	ctx := context.Background()
	blocker := lockSession(t, f.pool, f.session(t).id)
	if _, err := blocker.Exec(ctx, `UPDATE sessions SET revoked_at = now()`); err != nil {
		t.Fatal(err)
	}

	type result struct {
		res Credentials
		err error
	}
	done := make(chan result, 1)
	go func() {
		res, err := f.Refresh(ctx, f.login.RefreshToken.Raw)
		done <- result{res, err}
	}()
	waitForLockWaiters(t, f.pool, 1)
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	r := <-done
	requireInvalidRefresh(t, r.res, r.err)
}

// Concurrent logouts with one token all succeed; exactly one revokes (race 5).
func TestLogoutConcurrentSameToken(t *testing.T) {
	f := newRefreshFixture(t)
	const n = 20
	start := make(chan struct{})
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			<-start
			errs[i] = f.Logout(context.Background(), f.login.AccessToken.Raw)
		})
	}
	close(start)
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Errorf("logout: %v", err)
		}
	}
	if f.session(t).revokedAt == nil {
		t.Error("session not revoked")
	}
	requireLogoutLogsClean(t, f.testService, f.login)
	if got := strings.Count(f.logs.String(), "auth: logout succeeded"); got != 1 {
		t.Errorf("logout succeeded logged %d times, want 1", got)
	}
}

// Concurrent refreshes and logouts on one session never fail internally or
// deadlock, and always leave the session revoked: either a logout wins, or
// one refresh rotates and the others are reuse.
func TestLogoutRefreshStress(t *testing.T) {
	f := newRefreshFixture(t)
	const n = 10
	start := make(chan struct{})
	logoutErrs := make([]error, n)
	refreshErrs := make([]error, n)
	refreshed := make([]Credentials, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			<-start
			logoutErrs[i] = f.Logout(context.Background(), f.login.AccessToken.Raw)
		})
		wg.Go(func() {
			<-start
			refreshed[i], refreshErrs[i] = f.Refresh(context.Background(), f.login.RefreshToken.Raw)
		})
	}
	close(start)
	wg.Wait()
	for i := range n {
		if logoutErrs[i] != nil {
			t.Errorf("logout: %v", logoutErrs[i])
		}
		if err := refreshErrs[i]; err != nil && !errors.Is(err, ErrInvalidRefreshToken) {
			t.Errorf("refresh: unexpected error (deadlock or failure?): %v", err)
		}
	}
	if f.session(t).revokedAt == nil {
		t.Error("session live after concurrent logout and reuse")
	}
	requireLogoutLogsClean(t, f.testService, append(refreshed, f.login)...)
}
