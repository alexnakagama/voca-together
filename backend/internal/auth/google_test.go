package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"vocatogether/backend/internal/email"
	"vocatogether/backend/internal/testutil"
)

// ---- Store ----

// googleAttempt is one Google sign-in as the service will pass it to the
// store: a verified ID token (only its hash and exp reach the store) and the
// new session's tokens.
type googleAttempt struct {
	googleSignInInput
	rawIDToken      string
	access, refresh Token
}

// newGoogleAttempt returns an eligible attempt with a fresh ID token that
// expires in an hour (whole seconds, like a real exp claim).
func newGoogleAttempt(subject, addr string) googleAttempt {
	raw := "eyJhbGciOiJSUzI1NiJ9.test-payload." + NewToken("").Raw
	access, refresh := NewToken(AccessTokenPrefix), NewToken(RefreshTokenPrefix)
	return googleAttempt{
		googleSignInInput: googleSignInInput{
			tokenHash:   HashToken(raw),
			tokenExpiry: time.Unix(time.Now().Add(time.Hour).Unix(), 0),
			subject:     subject,
			eligible:    true,
			email:       addr,
			accessHash:  access.Hash,
			refreshHash: refresh.Hash,
			userAgent:   ptr(testUserAgent),
		},
		rawIDToken: raw,
		access:     access,
		refresh:    refresh,
	}
}

// withNewSession returns the attempt with the same ID token but new session
// tokens, as a replayed request would have.
func (a googleAttempt) withNewSession() googleAttempt {
	a.access, a.refresh = NewToken(AccessTokenPrefix), NewToken(RefreshTokenPrefix)
	a.accessHash, a.refreshHash = a.access.Hash, a.refresh.Hash
	return a
}

type googleTables struct{ users, identities, sessions, tokenUses int }

func countGoogleTables(t *testing.T, pool *pgxpool.Pool) googleTables {
	t.Helper()
	return googleTables{
		users:      countRows(t, pool, `SELECT count(*) FROM users`),
		identities: countRows(t, pool, `SELECT count(*) FROM user_identities`),
		sessions:   countRows(t, pool, `SELECT count(*) FROM sessions`),
		tokenUses:  countRows(t, pool, `SELECT count(*) FROM google_id_token_uses`),
	}
}

func requireGoogleTables(t *testing.T, pool *pgxpool.Pool, want googleTables) {
	t.Helper()
	if got := countGoogleTables(t, pool); got != want {
		t.Errorf("rows = %+v, want %+v", got, want)
	}
}

func requireGoogleOutcome(t *testing.T, r googleSignInResult, err error, want googleOutcome) {
	t.Helper()
	if err != nil {
		t.Fatalf("googleSignIn: %v", err)
	}
	if r.outcome != want {
		t.Fatalf("outcome = %d, want %d", r.outcome, want)
	}
}

func requirePgConstraint(t *testing.T, err error, code, constraint string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code || pgErr.ConstraintName != constraint {
		t.Fatalf("err = %v, want SQLSTATE %s on %s", err, code, constraint)
	}
}

func tokenUseExists(t *testing.T, pool *pgxpool.Pool, a googleAttempt) bool {
	t.Helper()
	return countRows(t, pool, `SELECT count(*) FROM google_id_token_uses WHERE token_hash = $1`, a.tokenHash) == 1
}

// userIDByEmail returns the id of the account with the address.
func userIDByEmail(t *testing.T, pool *pgxpool.Pool, addr string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, addr).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// usersAndIdentities dumps every user and identity row, to check that an
// outcome left accounts untouched.
func usersAndIdentities(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT u::text FROM users u UNION ALL SELECT t::text FROM user_tokens t
		 UNION ALL SELECT i::text FROM user_identities i ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	all, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(all, "\n")
}

// ---- Identity storage ----

func TestGoogleIdentityInsertAndLookup(t *testing.T) {
	pool := testutil.DB(t)
	ctx := context.Background()

	tx := beginTx(t, pool)
	userID, created, err := insertGoogleUserTx(ctx, tx, "gina@example.com")
	if err != nil || !created {
		t.Fatalf("insert user: created=%v err=%v", created, err)
	}
	if inserted, err := insertGoogleIdentityTx(ctx, tx, userID, "1001"); err != nil || !inserted {
		t.Fatalf("insert identity: inserted=%v err=%v", inserted, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	// A Google-created user has no password and counts as verified.
	u := loadUser(t, pool, "gina@example.com")
	if u.passwordHash != "" || u.verifiedAt == nil {
		t.Errorf("user: password %q, verified_at %v; want none and set", u.passwordHash, u.verifiedAt)
	}
	var provider, subject string
	if err := pool.QueryRow(ctx, `SELECT provider, subject FROM user_identities WHERE user_id = $1`, userID).
		Scan(&provider, &subject); err != nil || provider != "google" || subject != "1001" {
		t.Errorf("identity = (%q, %q), err = %v", provider, subject, err)
	}

	tx = beginTx(t, pool)
	got, found, err := lockUserByGoogleSubjectTx(ctx, tx, "1001")
	if err != nil || !found || got != userID {
		t.Fatalf("lookup: user=%s found=%v err=%v; want %s", got, found, err, userID)
	}
	// The lookup holds the user row FOR SHARE: a writer can't lock it.
	_, err = beginTx(t, pool).Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR UPDATE NOWAIT`, userID)
	requirePgConstraint(t, err, "55P03", "")

	for _, unknown := range []string{"1002", "100", "gina@example.com"} {
		if _, found, err := lockUserByGoogleSubjectTx(ctx, tx, unknown); err != nil || found {
			t.Errorf("lookup %q: found=%v err=%v; want not found", unknown, found, err)
		}
	}
}

// Subjects are unique per provider: a second user can't take one, and the
// conflict writes nothing.
func TestGoogleIdentityDuplicateSubject(t *testing.T) {
	pool := testutil.DB(t)
	ctx := context.Background()
	owner := seedGoogleAccount(t, pool, "gina@example.com", "1001")
	other := seedAccount(t, pool, "ana@example.com", "hash", true)

	tx := beginTx(t, pool)
	inserted, err := insertGoogleIdentityTx(ctx, tx, other, "1001")
	if err != nil || inserted {
		t.Fatalf("inserted=%v err=%v; want false, nil", inserted, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM user_identities WHERE user_id = $1 AND subject = '1001'`, owner); n != 1 {
		t.Errorf("owner's identity rows = %d, want 1", n)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM user_identities`); n != 1 {
		t.Errorf("identities = %d, want 1", n)
	}
}

// One Google identity per user. No flow links an existing user yet, so a
// second one is a bug, reported as an error rather than ignored.
func TestGoogleIdentitySecondForSameUser(t *testing.T) {
	pool := testutil.DB(t)
	ctx := context.Background()
	userID := seedGoogleAccount(t, pool, "gina@example.com", "1001")

	tx := beginTx(t, pool)
	_, err := insertGoogleIdentityTx(ctx, tx, userID, "1002")
	requirePgConstraint(t, err, "23505", "user_identities_user_provider_key")
}

func TestGoogleIdentityForeignKey(t *testing.T) {
	pool := testutil.DB(t)
	tx := beginTx(t, pool)
	_, err := insertGoogleIdentityTx(context.Background(), tx, "00000000-0000-0000-0000-000000000000", "1001")
	requirePgConstraint(t, err, "23503", "user_identities_user_id_fkey")
}

func TestGoogleIdentityDeletedWithUser(t *testing.T) {
	pool := testutil.DB(t)
	ctx := context.Background()
	userID := seedGoogleAccount(t, pool, "gina@example.com", "1001")

	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM user_identities`); n != 0 {
		t.Errorf("identities = %d, want 0", n)
	}
	tx := beginTx(t, pool)
	if _, found, err := lockUserByGoogleSubjectTx(ctx, tx, "1001"); err != nil || found {
		t.Errorf("lookup after delete: found=%v err=%v", found, err)
	}
}

// ---- googleSignIn: outcomes ----

func TestGoogleSignInCreatesAccount(t *testing.T) {
	pool := testutil.DB(t)
	ctx := context.Background()
	a := newGoogleAttempt("1001", "gina@outlook.example")

	r, err := googleSignIn(ctx, pool, a.googleSignInInput)
	requireGoogleOutcome(t, r, err, googleCreated)
	requireGoogleTables(t, pool, googleTables{users: 1, identities: 1, sessions: 1, tokenUses: 1})

	if r.userID != userIDByEmail(t, pool, "gina@outlook.example") {
		t.Errorf("userID %s is not the new account", r.userID)
	}
	u := loadUser(t, pool, "gina@outlook.example")
	if u.passwordHash != "" || u.verifiedAt == nil {
		t.Errorf("user: password %q, verified_at %v; want none and set", u.passwordHash, u.verifiedAt)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM user_identities WHERE user_id = $1 AND provider = 'google' AND subject = '1001'`, r.userID); n != 1 {
		t.Errorf("identity rows = %d, want 1", n)
	}
	// No verification email is pending: Google verified the address.
	if n := countRows(t, pool, `SELECT count(*) FROM user_tokens`); n != 0 {
		t.Errorf("user_tokens = %d, want 0", n)
	}

	// The session is exactly what password login creates.
	sessions := loadSessions(t, pool)
	if sessions[0].id != r.sessionID {
		t.Errorf("sessionID = %s, want %s", r.sessionID, sessions[0].id)
	}
	requireFreshSession(t, sessions[0], r.userID, a.access, a.refresh, a.userAgent)

	// Only the SHA-256 of the ID token is kept, expiring a minute after it.
	var storedExpiry time.Time
	if err := pool.QueryRow(ctx, `SELECT expires_at FROM google_id_token_uses WHERE token_hash = $1`, HashToken(a.rawIDToken)).
		Scan(&storedExpiry); err != nil {
		t.Fatal(err)
	}
	if want := a.tokenExpiry.Add(time.Minute); !storedExpiry.Equal(want) {
		t.Errorf("token use expires_at = %v, want exp + 1 min = %v", storedExpiry, want)
	}
	requireNoSecrets(t, "database", dumpTables(t, pool), []string{a.rawIDToken, "test-payload", a.access.Raw, a.refresh.Raw})
}

// A linked identity signs in to its user by subject alone: the token's
// current email and eligibility don't matter, and users.email isn't synced.
func TestGoogleSignInExistingIdentity(t *testing.T) {
	pool := testutil.DB(t)
	ctx := context.Background()
	userID := seedGoogleAccount(t, pool, "gina@example.com", "1001")
	before := usersAndIdentities(t, pool)

	for i, a := range []googleAttempt{
		newGoogleAttempt("1001", "gina@example.com"),
		newGoogleAttempt("1001", "renamed@example.com"),
		func() googleAttempt { a := newGoogleAttempt("1001", ""); a.eligible = false; return a }(),
	} {
		r, err := googleSignIn(ctx, pool, a.googleSignInInput)
		requireGoogleOutcome(t, r, err, googleSignedIn)
		if r.userID != userID || r.sessionID == "" {
			t.Errorf("attempt %d: user %s session %q; want %s and a session", i, r.userID, r.sessionID, userID)
		}
	}
	requireGoogleTables(t, pool, googleTables{users: 1, identities: 1, sessions: 3, tokenUses: 3})
	if after := usersAndIdentities(t, pool); after != before {
		t.Errorf("sign-in changed the account:\nbefore %s\nafter  %s", before, after)
	}
}

func TestGoogleSignInReplay(t *testing.T) {
	pool := testutil.DB(t)
	ctx := context.Background()
	a := newGoogleAttempt("1001", "gina@example.com")
	if r, err := googleSignIn(ctx, pool, a.googleSignInInput); err != nil || r.outcome != googleCreated {
		t.Fatalf("first use: %+v, %v", r, err)
	}
	before := dumpTables(t, pool)

	replay := a.withNewSession()
	r, err := googleSignIn(ctx, pool, replay.googleSignInInput)
	requireGoogleOutcome(t, r, err, googleReplayed)
	if r.userID != "" || r.sessionID != "" {
		t.Errorf("replay result = %+v, want no user or session", r)
	}
	if after := dumpTables(t, pool); after != before {
		t.Errorf("replay wrote something:\nbefore %s\nafter  %s", before, after)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM sessions WHERE access_token_hash = $1`, replay.access.Hash); n != 0 {
		t.Error("replay created a session")
	}
}

// A token is spent whatever the outcome it got: a replay after a refusal is
// still a replay, even once the refusal no longer applies.
func TestGoogleSignInRefusedTokenIsSpent(t *testing.T) {
	pool := testutil.DB(t)
	ctx := context.Background()

	ineligible := newGoogleAttempt("1001", "gina@example.com")
	ineligible.eligible = false
	r, err := googleSignIn(ctx, pool, ineligible.googleSignInInput)
	requireGoogleOutcome(t, r, err, googleIneligible)
	retry := ineligible.withNewSession()
	retry.eligible = true
	r, err = googleSignIn(ctx, pool, retry.googleSignInInput)
	requireGoogleOutcome(t, r, err, googleReplayed)

	seedAccount(t, pool, "ana@example.com", "hash", true)
	collision := newGoogleAttempt("1002", "ana@example.com")
	r, err = googleSignIn(ctx, pool, collision.googleSignInInput)
	requireGoogleOutcome(t, r, err, googleAccountExists)
	r, err = googleSignIn(ctx, pool, collision.withNewSession().googleSignInInput)
	requireGoogleOutcome(t, r, err, googleReplayed)

	requireGoogleTables(t, pool, googleTables{users: 1, identities: 0, sessions: 0, tokenUses: 2})
}

func TestGoogleSignInIneligibleWritesOnlyTokenUse(t *testing.T) {
	pool := testutil.DB(t)
	a := newGoogleAttempt("1001", "")
	a.eligible = false

	r, err := googleSignIn(context.Background(), pool, a.googleSignInInput)
	requireGoogleOutcome(t, r, err, googleIneligible)
	if r.userID != "" || r.sessionID != "" {
		t.Errorf("result = %+v, want no user or session", r)
	}
	requireGoogleTables(t, pool, googleTables{tokenUses: 1})
	if !tokenUseExists(t, pool, a) {
		t.Error("token use not committed")
	}
}

// An unknown identity whose email already has an account gets
// googleAccountExists, whatever kind of account it is. Nothing is linked and
// the account is untouched; only the token use is written.
func TestGoogleSignInAccountExists(t *testing.T) {
	pool := testutil.DB(t)
	ctx := context.Background()
	accounts := map[string]string{
		"verified@example.com":   seedAccount(t, pool, "verified@example.com", "hash-v", true),
		"unverified@example.com": seedAccount(t, pool, "unverified@example.com", "hash-u", false),
		"google@example.com":     seedGoogleAccount(t, pool, "google@example.com", "9999"),
	}
	before := usersAndIdentities(t, pool)
	sessionsBefore := sessionCount(t, pool)

	for addr, existing := range accounts {
		t.Run(addr, func(t *testing.T) {
			a := newGoogleAttempt("1001", addr)
			r, err := googleSignIn(ctx, pool, a.googleSignInInput)
			requireGoogleOutcome(t, r, err, googleAccountExists)
			if r.userID != existing || r.sessionID != "" {
				t.Errorf("result = %+v, want the existing user %s and no session", r, existing)
			}
			if !tokenUseExists(t, pool, a) {
				t.Error("token use not committed")
			}
		})
	}
	if after := usersAndIdentities(t, pool); after != before {
		t.Errorf("collision changed accounts:\nbefore %s\nafter  %s", before, after)
	}
	if n := sessionCount(t, pool); n != sessionsBefore {
		t.Errorf("sessions = %d, want %d", n, sessionsBefore)
	}
}

// ---- googleSignIn: atomicity ----

// Each injected failure must roll back everything the attempt wrote,
// including the token use, so the token isn't spent and works afterwards.
func TestGoogleSignInRollsBackOnFailure(t *testing.T) {
	for _, event := range []string{
		"BEFORE INSERT ON sessions",
		"BEFORE INSERT ON user_identities",
		"BEFORE INSERT ON users",
		"AFTER INSERT ON google_id_token_uses",
	} {
		t.Run(event, func(t *testing.T) {
			pool := testutil.DB(t)
			ctx := context.Background()
			remove := injectFailure(t, pool, event)
			a := newGoogleAttempt("1001", "gina@example.com")

			r, err := googleSignIn(ctx, pool, a.googleSignInInput)
			if err == nil || !strings.Contains(err.Error(), "injected failure") {
				t.Fatalf("result %+v, err = %v; want the injected failure", r, err)
			}
			if r != (googleSignInResult{}) {
				t.Errorf("result = %+v on failure, want zero", r)
			}
			requireGoogleTables(t, pool, googleTables{})

			remove()
			r, err = googleSignIn(ctx, pool, a.withNewSession().googleSignInInput)
			requireGoogleOutcome(t, r, err, googleCreated)
		})
	}
}

func TestGoogleSignInExistingIdentityRollsBackOnSessionFailure(t *testing.T) {
	pool := testutil.DB(t)
	seedGoogleAccount(t, pool, "gina@example.com", "1001")
	injectFailure(t, pool, "BEFORE INSERT ON sessions")
	a := newGoogleAttempt("1001", "gina@example.com")

	if _, err := googleSignIn(context.Background(), pool, a.googleSignInInput); err == nil {
		t.Fatal("want the injected failure")
	}
	requireGoogleTables(t, pool, googleTables{users: 1, identities: 1})
}

func TestGoogleSignInHonorsCancelledContext(t *testing.T) {
	pool := testutil.DB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := googleSignIn(ctx, pool, newGoogleAttempt("1001", "gina@example.com").googleSignInInput)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	requireGoogleTables(t, pool, googleTables{})
}

// Errors name the step, never the email, subject or token.
func TestGoogleSignInErrorsLeakNothing(t *testing.T) {
	pool := testutil.DB(t)
	injectFailure(t, pool, "BEFORE INSERT ON sessions")
	a := newGoogleAttempt("1001", "gina@example.com")
	_, err := googleSignIn(context.Background(), pool, a.googleSignInInput)
	if err == nil {
		t.Fatal("want an error")
	}
	requireNoSecrets(t, "error", err.Error(),
		append(tokenSecrets(a.rawIDToken), "gina@example.com", "1001", a.access.Raw, a.refresh.Raw))
}

// ---- googleSignIn: concurrency ----

// holdGoogleUser starts a transaction that has inserted a Google user with
// the identity but not committed, standing in for a concurrent first sign-in.
func holdGoogleUser(t *testing.T, pool *pgxpool.Pool, addr, subject string) (pgx.Tx, string) {
	t.Helper()
	ctx := context.Background()
	tx := beginTx(t, pool)
	userID, created, err := insertGoogleUserTx(ctx, tx, addr)
	if err != nil || !created {
		t.Fatalf("hold user: created=%v err=%v", created, err)
	}
	if inserted, err := insertGoogleIdentityTx(ctx, tx, userID, subject); err != nil || !inserted {
		t.Fatalf("hold identity: inserted=%v err=%v", inserted, err)
	}
	return tx, userID
}

type googleResultErr struct {
	r   googleSignInResult
	err error
}

func signInInBackground(pool *pgxpool.Pool, in googleSignInInput) <-chan googleResultErr {
	done := make(chan googleResultErr, 1)
	go func() {
		r, err := googleSignIn(context.Background(), pool, in)
		done <- googleResultErr{r, err}
	}()
	return done
}

// Same subject, other email, racing: the loser's identity insert waits for
// the winner, conflicts, and the retry signs in to the winner's user. The
// loser's email gets no account.
func TestGoogleSignInSameSubjectRaceRetries(t *testing.T) {
	pool := testutil.DB(t)
	ctx := context.Background()
	tx, winner := holdGoogleUser(t, pool, "first@example.com", "1001")

	a := newGoogleAttempt("1001", "second@example.com")
	done := signInInBackground(pool, a.googleSignInInput)
	waitForLockWaiters(t, pool, 1)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	res := <-done
	requireGoogleOutcome(t, res.r, res.err, googleSignedIn)
	if res.r.userID != winner {
		t.Errorf("signed in to %s, want the winner %s", res.r.userID, winner)
	}
	requireGoogleTables(t, pool, googleTables{users: 1, identities: 1, sessions: 1, tokenUses: 1})
	if n := countRows(t, pool, `SELECT count(*) FROM users WHERE email = 'second@example.com'`); n != 0 {
		t.Error("the loser's email got an account")
	}
}

// If the transaction holding the subject rolls back, the waiting attempt
// simply creates its own account.
func TestGoogleSignInSameSubjectRaceWinnerRollsBack(t *testing.T) {
	pool := testutil.DB(t)
	ctx := context.Background()
	tx, _ := holdGoogleUser(t, pool, "first@example.com", "1001")

	done := signInInBackground(pool, newGoogleAttempt("1001", "second@example.com").googleSignInInput)
	waitForLockWaiters(t, pool, 1)
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	res := <-done
	requireGoogleOutcome(t, res.r, res.err, googleCreated)
	if res.r.userID != userIDByEmail(t, pool, "second@example.com") {
		t.Error("account not created for the waiting attempt's email")
	}
	requireGoogleTables(t, pool, googleTables{users: 1, identities: 1, sessions: 1, tokenUses: 1})
}

// Same email, other subject: the waiting user insert sees the committed
// account and refuses to link (googleAccountExists). If the holder rolls
// back instead, the account is created.
func TestGoogleSignInConcurrentEmailCollision(t *testing.T) {
	for _, commit := range []bool{true, false} {
		t.Run(fmt.Sprintf("commit=%v", commit), func(t *testing.T) {
			pool := testutil.DB(t)
			ctx := context.Background()
			tx := beginTx(t, pool)
			var holder string
			if err := tx.QueryRow(ctx,
				`INSERT INTO users (email, password_hash) VALUES ('ana@example.com', 'hash') RETURNING id`).Scan(&holder); err != nil {
				t.Fatal(err)
			}

			done := signInInBackground(pool, newGoogleAttempt("1001", "ana@example.com").googleSignInInput)
			waitForLockWaiters(t, pool, 1)
			var res googleResultErr
			if commit {
				if err := tx.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				res = <-done
				requireGoogleOutcome(t, res.r, res.err, googleAccountExists)
				if res.r.userID != holder {
					t.Errorf("userID = %s, want the existing account %s", res.r.userID, holder)
				}
				requireGoogleTables(t, pool, googleTables{users: 1, tokenUses: 1})
				if loadUser(t, pool, "ana@example.com").passwordHash != "hash" {
					t.Error("existing account changed")
				}
			} else {
				if err := tx.Rollback(ctx); err != nil {
					t.Fatal(err)
				}
				res = <-done
				requireGoogleOutcome(t, res.r, res.err, googleCreated)
				requireGoogleTables(t, pool, googleTables{users: 1, identities: 1, sessions: 1, tokenUses: 1})
			}
		})
	}
}

// Every attempt that finds a linked identity waits for the user row only if
// someone holds it FOR UPDATE (a reset), and its session is created after
// that transaction, so a reset's revocation can't miss it.
func TestGoogleSignInWaitsForUserLock(t *testing.T) {
	pool := testutil.DB(t)
	ctx := context.Background()
	userID := seedGoogleAccount(t, pool, "gina@example.com", "1001")
	insertSession(t, pool, userID, 30*day, 90*day, nil)

	tx := lockUser(t, pool, "gina@example.com")
	done := signInInBackground(pool, newGoogleAttempt("1001", "gina@example.com").googleSignInInput)
	waitForLockWaiters(t, pool, 1)
	if _, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE user_id = $1`, userID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	res := <-done
	requireGoogleOutcome(t, res.r, res.err, googleSignedIn)
	if n := countRows(t, pool, `SELECT count(*) FROM sessions WHERE id = $1 AND revoked_at IS NULL`, res.r.sessionID); n != 1 {
		t.Error("the session created after the lock was released is not live")
	}
}

// The retry happens once. Here every identity insert is silently skipped by
// a trigger, so each attempt looks like a lost race: the second one is
// returned as an error and nothing is committed.
func TestGoogleSignInRetriesOnlyOnce(t *testing.T) {
	pool := testutil.DB(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		CREATE FUNCTION test_skip_insert() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NULL; END $$;
		CREATE TRIGGER test_skip_insert BEFORE INSERT ON user_identities FOR EACH ROW EXECUTE FUNCTION test_skip_insert()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `DROP TRIGGER test_skip_insert ON user_identities; DROP FUNCTION test_skip_insert()`); err != nil {
			t.Errorf("remove trigger: %v", err)
		}
	})

	_, err := googleSignIn(ctx, pool, newGoogleAttempt("1001", "gina@example.com").googleSignInInput)
	if !errors.Is(err, errGoogleIdentityRace) {
		t.Fatalf("err = %v, want errGoogleIdentityRace", err)
	}
	requireGoogleTables(t, pool, googleTables{})
}

// runConcurrently calls googleSignIn for every attempt at once and returns
// the results in order. Each call has a deadline, so a deadlock or a hang
// fails the test instead of blocking it (PostgreSQL would also abort one side
// of a deadlock with 40P01).
func runConcurrently(t *testing.T, pool *pgxpool.Pool, attempts []googleAttempt) []googleResultErr {
	t.Helper()
	results := make([]googleResultErr, len(attempts))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, a := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			<-start
			r, err := googleSignIn(ctx, pool, a.googleSignInInput)
			results[i] = googleResultErr{r, err}
		}()
	}
	close(start)
	wg.Wait()
	return results
}

func countOutcomes(t *testing.T, results []googleResultErr) map[googleOutcome]int {
	t.Helper()
	counts := map[googleOutcome]int{}
	for i, res := range results {
		if res.err != nil {
			t.Errorf("attempt %d: %v", i, res.err)
			continue
		}
		counts[res.r.outcome]++
	}
	return counts
}

const concurrentAttempts = 12

func TestGoogleSignInConcurrentSameToken(t *testing.T) {
	pool := testutil.DB(t)
	a := newGoogleAttempt("1001", "gina@example.com")
	attempts := make([]googleAttempt, concurrentAttempts)
	for i := range attempts {
		attempts[i] = a.withNewSession()
	}

	counts := countOutcomes(t, runConcurrently(t, pool, attempts))
	if counts[googleCreated] != 1 || counts[googleReplayed] != concurrentAttempts-1 {
		t.Errorf("outcomes = %v, want 1 created and %d replayed", counts, concurrentAttempts-1)
	}
	requireGoogleTables(t, pool, googleTables{users: 1, identities: 1, sessions: 1, tokenUses: 1})
}

// First sign-ins of one subject with different tokens: one account, one
// identity, a session each.
func TestGoogleSignInConcurrentSameSubject(t *testing.T) {
	pool := testutil.DB(t)
	attempts := make([]googleAttempt, concurrentAttempts)
	for i := range attempts {
		attempts[i] = newGoogleAttempt("1001", "gina@example.com")
	}

	counts := countOutcomes(t, runConcurrently(t, pool, attempts))
	if counts[googleCreated] != 1 || counts[googleSignedIn] != concurrentAttempts-1 {
		t.Errorf("outcomes = %v, want 1 created and %d signed in", counts, concurrentAttempts-1)
	}
	requireGoogleTables(t, pool, googleTables{users: 1, identities: 1, sessions: concurrentAttempts, tokenUses: concurrentAttempts})
}

// One subject, a different email per attempt (the retry path): still one
// account, whose email is one of them and stays the same afterwards.
func TestGoogleSignInConcurrentSameSubjectConflictingEmails(t *testing.T) {
	pool := testutil.DB(t)
	attempts := make([]googleAttempt, concurrentAttempts)
	for i := range attempts {
		attempts[i] = newGoogleAttempt("1001", fmt.Sprintf("user%d@example.com", i))
	}

	results := runConcurrently(t, pool, attempts)
	counts := countOutcomes(t, results)
	if counts[googleCreated] != 1 || counts[googleSignedIn] != concurrentAttempts-1 {
		t.Errorf("outcomes = %v, want 1 created and %d signed in", counts, concurrentAttempts-1)
	}
	requireGoogleTables(t, pool, googleTables{users: 1, identities: 1, sessions: concurrentAttempts, tokenUses: concurrentAttempts})
	var winner string
	for i, res := range results {
		if res.r.outcome == googleCreated {
			winner = attempts[i].email
		}
	}
	if n := countRows(t, pool, `SELECT count(*) FROM users WHERE email = $1`, winner); n != 1 {
		t.Errorf("the account doesn't have the creating attempt's email %q", winner)
	}
}

// Different subjects, one email: the first creates the account; the others
// are refused without linking.
func TestGoogleSignInConcurrentSameEmail(t *testing.T) {
	pool := testutil.DB(t)
	attempts := make([]googleAttempt, concurrentAttempts)
	for i := range attempts {
		attempts[i] = newGoogleAttempt(fmt.Sprint(1000+i), "gina@example.com")
	}

	counts := countOutcomes(t, runConcurrently(t, pool, attempts))
	if counts[googleCreated] != 1 || counts[googleAccountExists] != concurrentAttempts-1 {
		t.Errorf("outcomes = %v, want 1 created and %d account_exists", counts, concurrentAttempts-1)
	}
	requireGoogleTables(t, pool, googleTables{users: 1, identities: 1, sessions: 1, tokenUses: concurrentAttempts})
}

// A mix of subjects, emails, shared tokens and an existing password account,
// all at once, repeated: no errors (so no deadlock), and the invariants hold.
func TestGoogleSignInConcurrentMixNoDeadlock(t *testing.T) {
	pool := testutil.DB(t)
	seedAccount(t, pool, "taken@example.com", "hash", true)
	subjects := []string{"1001", "1002", "1003"}
	emails := []string{"a@example.com", "b@example.com", "taken@example.com"}
	for round := range 5 {
		shared := newGoogleAttempt(subjects[round%3], emails[round%2])
		var attempts []googleAttempt
		for i := range 3 * concurrentAttempts {
			if i%4 == 0 {
				attempts = append(attempts, shared.withNewSession())
				continue
			}
			attempts = append(attempts, newGoogleAttempt(subjects[i%3], emails[(i/3)%3]))
		}
		countOutcomes(t, runConcurrently(t, pool, attempts))
	}

	if n := countRows(t, pool, `SELECT count(*) FROM user_identities`); n > len(subjects) {
		t.Errorf("identities = %d, want at most one per subject", n)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM users u WHERE password_hash IS NULL
		AND NOT EXISTS (SELECT 1 FROM user_identities i WHERE i.user_id = u.id)`); n != 0 {
		t.Errorf("%d passwordless users without an identity", n)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM user_identities i JOIN users u ON u.id = i.user_id
		WHERE u.email = 'taken@example.com'`); n != 0 {
		t.Error("the password account was linked")
	}
	if n := countRows(t, pool, `SELECT count(*) FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE u.email = 'taken@example.com'`); n != 0 {
		t.Error("the password account got a session")
	}
}

// ---- Compatibility with password auth and sessions ----

// A Google session is an ordinary vt session: authentication, /v1/me data,
// refresh rotation with reuse detection, and logout all work unchanged.
func TestGoogleSessionWorksWithSessionMachinery(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	ctx := context.Background()
	a := newGoogleAttempt("1001", "gina@example.com")
	r, err := googleSignIn(ctx, s.pool, a.googleSignInInput)
	requireGoogleOutcome(t, r, err, googleCreated)

	id, err := s.Authenticate(ctx, a.access.Raw)
	if err != nil || id != (Identity{UserID: r.userID, SessionID: r.sessionID}) {
		t.Fatalf("Authenticate = %+v, %v", id, err)
	}
	u, err := s.User(ctx, id.UserID)
	if err != nil || u.Email != "gina@example.com" || u.EmailVerifiedAt.IsZero() {
		t.Fatalf("User = %+v, %v", u, err)
	}

	creds, err := s.Refresh(ctx, a.refresh.Raw)
	if err != nil || creds.ExpiresIn != accessTokenTTL {
		t.Fatalf("Refresh: expires_in %v, err %v", creds.ExpiresIn, err)
	}
	if _, err := s.Authenticate(ctx, a.access.Raw); !errors.Is(err, ErrInvalidAccessToken) {
		t.Errorf("rotated-out access token: err = %v", err)
	}
	if err := s.Logout(ctx, creds.AccessToken.Raw); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := s.Authenticate(ctx, creds.AccessToken.Raw); !errors.Is(err, ErrInvalidAccessToken) {
		t.Errorf("after logout: err = %v", err)
	}
	if _, err := s.Refresh(ctx, creds.RefreshToken.Raw); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Errorf("refresh after logout: err = %v", err)
	}
}

// Password login can't create a session for a Google-created account: its
// NULL password_hash never matches createSession's re-check.
func TestCreateSessionRefusesPasswordlessAccount(t *testing.T) {
	pool := testutil.DB(t)
	r, err := googleSignIn(context.Background(), pool, newGoogleAttempt("1001", "gina@example.com").googleSignInInput)
	requireGoogleOutcome(t, r, err, googleCreated)

	for _, hash := range []string{"", "hash", HashPassword(testPassword)} {
		_, err := createSession(context.Background(), pool, r.userID, hash,
			NewToken(AccessTokenPrefix).Hash, NewToken(RefreshTokenPrefix).Hash, nil)
		if !errors.Is(err, errPasswordChanged) {
			t.Errorf("hash %q: err = %v, want errPasswordChanged", hash, err)
		}
	}
	if n := sessionCount(t, pool); n != 1 {
		t.Errorf("sessions = %d, want only the Google one", n)
	}
}

func TestGoogleCreatedAccountWithPasswordAuth(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	ctx := context.Background()
	r, err := googleSignIn(ctx, s.pool, newGoogleAttempt("1001", "gina@example.com").googleSignInInput)
	requireGoogleOutcome(t, r, err, googleCreated)

	// Password login: the uniform 401.
	if _, err := s.Login(ctx, "gina@example.com", testPassword, testUserAgent); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("Login: err = %v, want ErrInvalidCredentials", err)
	}
	// Forgot password: no reset token (decision 020).
	if _, req, err := issuePasswordResetToken(ctx, s.pool, "gina@example.com", NewToken("").Hash, passwordResetTokenTTL); err != nil || req != resetPasswordless {
		t.Errorf("issuePasswordResetToken = %v, %v; want resetPasswordless", req, err)
	}
	if n := countRows(t, s.pool, `SELECT count(*) FROM user_tokens`); n != 0 {
		t.Errorf("user_tokens = %d, want 0", n)
	}
	// The auth-method trigger still guards the account.
	_, err = s.pool.Exec(ctx, `DELETE FROM user_identities WHERE user_id = $1`, r.userID)
	requirePgConstraint(t, err, "23514", "users_auth_method_required")
	if n := sessionCount(t, s.pool); n != 1 {
		t.Errorf("sessions = %d, want 1", n)
	}
}

// ---- Cleanup ----

func insertGoogleTokenUse(t *testing.T, pool *pgxpool.Pool, expiresIn time.Duration) []byte {
	t.Helper()
	h := NewToken("").Hash
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO google_id_token_uses (token_hash, expires_at) VALUES ($1, now() + make_interval(secs => $2))`,
		h, expiresIn.Seconds()); err != nil {
		t.Fatal(err)
	}
	return h
}

func googleTokenUseLeft(t *testing.T, pool *pgxpool.Pool, h []byte) bool {
	t.Helper()
	return countRows(t, pool, `SELECT count(*) FROM google_id_token_uses WHERE token_hash = $1`, h) == 1
}

// Token uses go as soon as they expire (no retention): the verifier rejects
// the token by then.
func TestCleanupDeletesExpiredGoogleTokenUses(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	expired := [][]byte{insertGoogleTokenUse(t, s.pool, -time.Second), insertGoogleTokenUse(t, s.pool, -31*day)}
	live := insertGoogleTokenUse(t, s.pool, time.Minute)

	sessions, tokens, googleTokens, err := s.cleanup(context.Background())
	if err != nil || sessions != 0 || tokens != 0 || googleTokens != 2 {
		t.Fatalf("cleanup = %d, %d, %d, %v; want 0, 0, 2", sessions, tokens, googleTokens, err)
	}
	for _, h := range expired {
		if googleTokenUseLeft(t, s.pool, h) {
			t.Error("expired token use survived")
		}
	}
	if !googleTokenUseLeft(t, s.pool, live) {
		t.Error("live token use deleted")
	}
	if _, _, googleTokens, err := s.cleanup(context.Background()); err != nil || googleTokens != 0 {
		t.Errorf("second run: %d, %v", googleTokens, err)
	}
}

// A row a sign-in stored keeps its token replay-proof until expires_at.
func TestCleanupKeepsGoogleTokenUseUntilExpiry(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	ctx := context.Background()
	a := newGoogleAttempt("1001", "gina@example.com")
	a.tokenExpiry = time.Now().Add(-30 * time.Second) // within the skew: still accepted by the verifier
	r, err := googleSignIn(ctx, s.pool, a.googleSignInInput)
	requireGoogleOutcome(t, r, err, googleCreated)

	if _, _, n, err := s.cleanup(ctx); err != nil || n != 0 {
		t.Fatalf("cleanup deleted %d token uses, err %v; want 0", n, err)
	}
	r, err = googleSignIn(ctx, s.pool, a.withNewSession().googleSignInInput)
	requireGoogleOutcome(t, r, err, googleReplayed)
}

func TestCleanupGoogleTokenUsesInBatches(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	for range 5 {
		insertGoogleTokenUse(t, s.pool, -time.Second)
	}
	s.cleanupBatchSize = 2
	if _, _, n, err := s.cleanup(context.Background()); err != nil || n != 5 {
		t.Fatalf("deleted %d, err %v; want 5 over several batches", n, err)
	}
}

func TestCleanupSkipsLockedGoogleTokenUses(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	ctx := context.Background()
	h := insertGoogleTokenUse(t, s.pool, -time.Second)
	tx := beginTx(t, s.pool)
	if _, err := tx.Exec(ctx, `SELECT 1 FROM google_id_token_uses WHERE token_hash = $1 FOR UPDATE`, h); err != nil {
		t.Fatal(err)
	}

	runCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, _, n, err := s.cleanup(runCtx); err != nil || n != 0 {
		t.Fatalf("cleanup blocked or deleted a locked row: %d, %v", n, err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, n, err := s.cleanup(ctx); err != nil || n != 1 {
		t.Errorf("after unlock: %d, %v; want 1", n, err)
	}
}

func TestRunCleanupLogsGoogleTokenUses(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	insertGoogleTokenUse(t, s.pool, -time.Second)

	logs := runCleanupUntilLogged(t, s, time.Hour)
	if n := countRows(t, s.pool, `SELECT count(*) FROM google_id_token_uses`); n != 0 {
		t.Errorf("token uses = %d after a logged run, want 0", n)
	}
	if !strings.Contains(logs, "google_token_uses_deleted=1") {
		t.Errorf("cleanup log lacks the token use count:\n%s", logs)
	}
}
