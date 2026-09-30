package auth

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"vocatogether/backend/internal/email"
)

// insertSession stores a session whose times are offsets from now:
// refreshEnd and end are its refresh and absolute expiries (the access token
// expires an hour before refreshEnd), revoked (if non-nil) its revocation
// time. It returns the session id.
func insertSession(t *testing.T, pool *pgxpool.Pool, userID string, refreshEnd, end time.Duration, revoked *time.Duration) string {
	t.Helper()
	var revokedSecs *float64
	if revoked != nil {
		v := revoked.Seconds()
		revokedSecs = &v
	}
	a, r := NewToken(AccessTokenPrefix), NewToken(RefreshTokenPrefix)
	var id string
	err := pool.QueryRow(context.Background(),
		`INSERT INTO sessions (user_id, access_token_hash, access_expires_at, refresh_token_hash,
		                       refresh_expires_at, expires_at, revoked_at)
		 VALUES ($1, $2, now() + make_interval(secs => $4) - interval '1 hour', $3,
		         now() + make_interval(secs => $4), now() + make_interval(secs => $5),
		         now() + make_interval(secs => $6))
		 RETURNING id`,
		userID, a.Hash, r.Hash, refreshEnd.Seconds(), end.Seconds(), revokedSecs).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func insertToken(t *testing.T, pool *pgxpool.Pool, userID, purpose string, expiresIn time.Duration, used bool) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(),
		`INSERT INTO user_tokens (user_id, purpose, token_hash, expires_at, used_at)
		 VALUES ($1, $2, $3, now() + make_interval(secs => $4), CASE WHEN $5 THEN now() - interval '40 days' END)
		 RETURNING id`,
		userID, purpose, NewToken("").Hash, expiresIn.Seconds(), used).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func exists(t *testing.T, pool *pgxpool.Pool, table, id string) bool {
	t.Helper()
	return countRows(t, pool, `SELECT count(*) FROM `+table+` WHERE id = $1`, id) == 1
}

const day = 24 * time.Hour

func TestCleanupDeletesOnlyRowsPastRetention(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	userID := seedAccount(t, s.pool, "ana@example.com", "hash", true)
	other := seedAccount(t, s.pool, "bob@example.com", "hash", false)
	// seedAccount's own tokens (used or live) are recent: they must survive.
	recentTokens := countRows(t, s.pool, `SELECT count(*) FROM user_tokens`)

	old := -31 * day
	recent := -29 * day
	live := insertSession(t, s.pool, userID, 30*day, 90*day, nil)
	absoluteOld := insertSession(t, s.pool, userID, old, old, nil)
	absoluteRecent := insertSession(t, s.pool, userID, recent, recent, nil)
	slidingOld := insertSession(t, s.pool, userID, old, 10*day, nil)
	slidingRecent := insertSession(t, s.pool, userID, recent, 10*day, nil)
	revokedOld := insertSession(t, s.pool, userID, 30*day, 90*day, &old)
	revokedRecent := insertSession(t, s.pool, userID, 30*day, 90*day, &recent)

	expiredOld := insertToken(t, s.pool, other, PurposePasswordReset, old, false)
	usedOld := insertToken(t, s.pool, userID, PurposePasswordReset, old, true)
	expiredRecent := insertToken(t, s.pool, userID, PurposeEmailVerification, recent, true)

	sessions, tokens, err := s.cleanup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sessions != 3 || tokens != 2 {
		t.Errorf("deleted sessions = %d, tokens = %d; want 3 and 2", sessions, tokens)
	}
	for _, id := range []string{absoluteOld, slidingOld, revokedOld} {
		if exists(t, s.pool, "sessions", id) {
			t.Errorf("session %s past retention survived", id)
		}
	}
	for _, id := range []string{live, absoluteRecent, slidingRecent, revokedRecent} {
		if !exists(t, s.pool, "sessions", id) {
			t.Errorf("session %s within retention was deleted", id)
		}
	}
	for _, id := range []string{expiredOld, usedOld} {
		if exists(t, s.pool, "user_tokens", id) {
			t.Errorf("token %s past retention survived", id)
		}
	}
	if !exists(t, s.pool, "user_tokens", expiredRecent) {
		t.Error("token within retention was deleted")
	}
	if n := countRows(t, s.pool, `SELECT count(*) FROM user_tokens`); n != recentTokens+1 {
		t.Errorf("tokens left = %d, want %d", n, recentTokens+1)
	}
	if n := countRows(t, s.pool, `SELECT count(*) FROM users`); n != 2 {
		t.Errorf("users = %d, cleanup must not delete accounts", n)
	}

	// Idempotent.
	if sessions, tokens, err := s.cleanup(context.Background()); err != nil || sessions != 0 || tokens != 0 {
		t.Errorf("second run: sessions = %d, tokens = %d, err = %v", sessions, tokens, err)
	}
}

func TestCleanupWorksInBatches(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	userID := seedAccount(t, s.pool, "ana@example.com", "hash", true)
	old := -31 * day
	for range 5 {
		insertSession(t, s.pool, userID, old, old, nil)
	}
	s.cleanupBatchSize = 2
	sessions, _, err := s.cleanup(context.Background())
	if err != nil || sessions != 5 {
		t.Fatalf("sessions = %d, err = %v; want 5 over several batches", sessions, err)
	}
}

// Cleanup never waits for a lock (SKIP LOCKED), so it can't take part in a
// deadlock with reset, resend or forgot, which lock the user row first and
// then token rows. Locked rows are left for the next run.
func TestCleanupSkipsLockedRows(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	userID := seedAccount(t, s.pool, "ana@example.com", "hash", true)
	old := -31 * day
	sessionID := insertSession(t, s.pool, userID, old, old, nil)
	tokenID := insertToken(t, s.pool, userID, PurposePasswordReset, old, false)

	tx := beginTx(t, s.pool)
	ctx := context.Background()
	if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR UPDATE`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SELECT 1 FROM sessions WHERE id = $1 FOR UPDATE`, sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SELECT 1 FROM user_tokens WHERE id = $1 FOR UPDATE`, tokenID); err != nil {
		t.Fatal(err)
	}

	runCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	sessions, tokens, err := s.cleanup(runCtx)
	if err != nil {
		t.Fatalf("cleanup blocked or failed: %v", err)
	}
	if sessions != 0 || tokens != 0 {
		t.Errorf("deleted locked rows: sessions = %d, tokens = %d", sessions, tokens)
	}

	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if sessions, tokens, err := s.cleanup(ctx); err != nil || sessions != 1 || tokens != 1 {
		t.Errorf("after unlock: sessions = %d, tokens = %d, err = %v; want 1 and 1", sessions, tokens, err)
	}
}

func TestRunCleanupRunsPeriodicallyAndStops(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	userID := seedAccount(t, s.pool, "ana@example.com", "hash", true)
	insertSession(t, s.pool, userID, -31*day, -31*day, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.RunCleanup(ctx, time.Millisecond, 10*time.Millisecond)
		close(done)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for countRows(t, s.pool, `SELECT count(*) FROM sessions`) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("cleanup did not run")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunCleanup did not stop when its context ended")
	}

	logs := s.logs.String()
	if !strings.Contains(logs, "auth: cleanup") || !strings.Contains(logs, "sessions_deleted=1") {
		t.Errorf("cleanup not logged with counts:\n%s", logs)
	}
	if strings.Contains(logs, "ana@example.com") {
		t.Errorf("cleanup log contains an email:\n%s", logs)
	}
}
