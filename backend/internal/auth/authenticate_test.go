package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func requireInvalidAccess(t *testing.T, id Identity, err error) {
	t.Helper()
	if !errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("err = %v, want ErrInvalidAccessToken", err)
	}
	if id != (Identity{}) {
		t.Errorf("identity %+v returned with an error", id)
	}
}

// rejectionLog returns the log line of the access-token rejection, if any.
func rejectionLog(logs string) string {
	for line := range strings.Lines(logs) {
		if strings.Contains(line, "auth: access token rejected") {
			return line
		}
	}
	return ""
}

func TestAuthenticate(t *testing.T) {
	f := newRefreshFixture(t)
	before := f.session(t)

	id, err := f.Authenticate(context.Background(), f.login.AccessToken.Raw)
	if err != nil {
		t.Fatal(err)
	}
	if want := (Identity{UserID: f.userID, SessionID: before.id}); id != want {
		t.Errorf("identity = %+v, want %+v", id, want)
	}

	// A read only: last_used_at and everything else stay as they were.
	requireUnchanged(t, before, f.session(t))
	requireLogoutLogsClean(t, f.testService, f.login)
}

func TestAuthenticateRejectsMalformedTokens(t *testing.T) {
	f := newRefreshFixture(t)
	for name, raw := range map[string]string{
		"empty":         "",
		"refresh token": f.login.RefreshToken.Raw,
		"no prefix":     strings.TrimPrefix(f.login.AccessToken.Raw, AccessTokenPrefix),
		"garbage":       AccessTokenPrefix + "garbage",
		"trailing":      f.login.AccessToken.Raw + " ",
	} {
		t.Run(name, func(t *testing.T) {
			id, err := f.Authenticate(context.Background(), raw)
			requireInvalidAccess(t, id, err)
		})
	}
	requireLogoutLogsClean(t, f.testService, f.login)
	if !strings.Contains(f.logs.String(), "reason=malformed") {
		t.Errorf("rejection not logged: %s", f.logs)
	}
}

func TestAuthenticateRejectsUnusableTokens(t *testing.T) {
	for name, setup := range map[string]func(t *testing.T, f refreshFixture){
		"unknown": func(t *testing.T, f refreshFixture) {
			if _, err := f.pool.Exec(context.Background(), `DELETE FROM sessions`); err != nil {
				t.Fatal(err)
			}
		},
		"revoked": func(t *testing.T, f refreshFixture) {
			if err := f.Logout(context.Background(), f.login.AccessToken.Raw); err != nil {
				t.Fatal(err)
			}
		},
		"access expired": func(t *testing.T, f refreshFixture) {
			if _, err := f.pool.Exec(context.Background(),
				`UPDATE sessions SET access_expires_at = now() - interval '1 second'`); err != nil {
				t.Fatal(err)
			}
		},
		"session expired": func(t *testing.T, f refreshFixture) {
			setSessionExpiry(t, f.pool, f.session(t).id, -time.Minute)
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newRefreshFixture(t)
			setup(t, f)
			id, err := f.Authenticate(context.Background(), f.login.AccessToken.Raw)
			requireInvalidAccess(t, id, err)
			requireLogoutLogsClean(t, f.testService, f.login)
			line := rejectionLog(f.logs.String())
			if !strings.Contains(line, "reason=invalid") || strings.Contains(line, f.userID) || strings.Contains(line, "session_id") {
				t.Errorf("rejection not logged, or logged with ids: %q", line)
			}
		})
	}
}

func TestAuthenticateRejectsRotatedOutToken(t *testing.T) {
	f := newRefreshFixture(t)
	res, err := f.Refresh(context.Background(), f.login.RefreshToken.Raw)
	if err != nil {
		t.Fatal(err)
	}

	id, err := f.Authenticate(context.Background(), f.login.AccessToken.Raw)
	requireInvalidAccess(t, id, err)

	id, err = f.Authenticate(context.Background(), res.AccessToken.Raw)
	if err != nil {
		t.Fatal(err)
	}
	if id.SessionID != f.session(t).id || id.UserID != f.userID {
		t.Errorf("identity = %+v", id)
	}
	requireLogoutLogsClean(t, f.testService, f.login, res)
}

func TestAuthenticateDistinguishesSessions(t *testing.T) {
	f := newRefreshFixture(t)
	benID := seedAccount(t, f.pool, "ben@example.com", HashPassword(testPassword), true)
	ben, err := f.Login(context.Background(), "ben@example.com", testPassword, "")
	if err != nil {
		t.Fatal(err)
	}

	ana, err := f.Authenticate(context.Background(), f.login.AccessToken.Raw)
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.Authenticate(context.Background(), ben.AccessToken.Raw)
	if err != nil {
		t.Fatal(err)
	}
	if ana.UserID != f.userID || got.UserID != benID || ana.SessionID == got.SessionID {
		t.Errorf("ana = %+v, ben = %+v", ana, got)
	}
}

// Authentication doesn't lock: a refresh holding the session row doesn't
// block it, and it sees the last committed tokens.
func TestAuthenticateDoesNotWaitForRefresh(t *testing.T) {
	f := newRefreshFixture(t)
	tx := lockSession(t, f.pool, f.session(t).id)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := f.Authenticate(ctx, f.login.AccessToken.Raw); err != nil {
		t.Fatalf("blocked or failed behind a row lock: %v", err)
	}
	_ = tx.Rollback(context.Background())
}

// Racing a refresh, each authentication either succeeds (before the rotation
// committed) or is rejected (after), never fails otherwise. Afterwards only
// the new token works.
func TestAuthenticateConcurrentWithRefresh(t *testing.T) {
	f := newRefreshFixture(t)
	const n = 20
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			_, err := f.Authenticate(context.Background(), f.login.AccessToken.Raw)
			errs <- err
		})
	}
	res, refreshErr := f.Refresh(context.Background(), f.login.RefreshToken.Raw)
	wg.Wait()
	close(errs)
	if refreshErr != nil {
		t.Fatal(refreshErr)
	}
	for err := range errs {
		if err != nil && !errors.Is(err, ErrInvalidAccessToken) {
			t.Errorf("err = %v", err)
		}
	}

	id, err := f.Authenticate(context.Background(), f.login.AccessToken.Raw)
	requireInvalidAccess(t, id, err)
	if _, err := f.Authenticate(context.Background(), res.AccessToken.Raw); err != nil {
		t.Fatal(err)
	}
	requireLogoutLogsClean(t, f.testService, f.login, res)
}

func TestAuthenticateInternalError(t *testing.T) {
	f := newRefreshFixture(t)
	f.pool.Close()
	id, err := f.Authenticate(context.Background(), f.login.AccessToken.Raw)
	if err == nil || errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("err = %v, want an internal error", err)
	}
	if id != (Identity{}) {
		t.Errorf("identity %+v returned with an error", id)
	}
	if strings.Contains(err.Error(), f.login.AccessToken.Raw) {
		t.Errorf("error contains the token: %v", err)
	}
}

func TestAuthenticateCanceledContext(t *testing.T) {
	f := newRefreshFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Authenticate(ctx, f.login.AccessToken.Raw); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestUser(t *testing.T) {
	f := newRefreshFixture(t)
	var want User
	if err := f.pool.QueryRow(context.Background(),
		`SELECT id, email, email_verified_at, created_at FROM users WHERE id = $1`, f.userID).
		Scan(&want.ID, &want.Email, &want.EmailVerifiedAt, &want.CreatedAt); err != nil {
		t.Fatal(err)
	}

	got, err := f.User(context.Background(), f.userID)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("user = %+v, want %+v", got, want)
	}
	if got.Email != "ana@example.com" || got.EmailVerifiedAt.IsZero() || got.CreatedAt.IsZero() {
		t.Errorf("user = %+v", got)
	}
}

// A user deleted after authentication has no sessions left (ON DELETE
// CASCADE), so the credential is treated as dead rather than as a failure.
func TestUserDeletedAfterAuthentication(t *testing.T) {
	f := newRefreshFixture(t)
	id, err := f.Authenticate(context.Background(), f.login.AccessToken.Raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.User(context.Background(), id.UserID); !errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("err = %v, want ErrInvalidAccessToken", err)
	}
}

func TestUserInternalError(t *testing.T) {
	f := newRefreshFixture(t)
	f.pool.Close()
	if _, err := f.User(context.Background(), f.userID); err == nil || errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("err = %v, want an internal error", err)
	}
}
