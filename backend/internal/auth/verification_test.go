package auth

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"vocatogether/backend/internal/email"
)

// seedUser creates an unverified user with a live verification token through
// the store (no argon2, no email) and returns the token.
func seedUser(t *testing.T, pool *pgxpool.Pool, addr string) Token {
	t.Helper()
	tok := NewToken("")
	created, err := createUserWithVerificationToken(context.Background(), pool, addr, "hash-of-"+addr, tok.Hash, verificationTokenTTL)
	if err != nil || !created {
		t.Fatalf("seed user: created=%v err=%v", created, err)
	}
	return tok
}

type userRow struct {
	verifiedAt   *time.Time
	updatedAt    time.Time
	passwordHash string
}

func loadUser(t *testing.T, pool *pgxpool.Pool, addr string) userRow {
	t.Helper()
	var u userRow
	err := pool.QueryRow(context.Background(),
		`SELECT email_verified_at, updated_at, password_hash FROM users WHERE email = $1`, addr).
		Scan(&u.verifiedAt, &u.updatedAt, &u.passwordHash)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func unusedTokens(t *testing.T, pool *pgxpool.Pool, addr string) int {
	t.Helper()
	return countRows(t, pool,
		`SELECT count(*) FROM user_tokens t JOIN users u ON u.id = t.user_id
		 WHERE u.email = $1 AND t.used_at IS NULL`, addr)
}

func tokenUsed(t *testing.T, pool *pgxpool.Pool, tok Token) bool {
	t.Helper()
	var used bool
	if err := pool.QueryRow(context.Background(),
		`SELECT used_at IS NOT NULL FROM user_tokens WHERE token_hash = $1`, tok.Hash).Scan(&used); err != nil {
		t.Fatal(err)
	}
	return used
}

func tokenIn(t *testing.T, msg email.Message) string {
	t.Helper()
	return linkIn(t, msg.Text).Query().Get("token")
}

func isTokenInvalid(err error) bool {
	var verr *ValidationError
	return errors.As(err, &verr) && slices.Equal(verr.Fields, []*FieldError{ErrTokenInvalid})
}

func requireTokenInvalid(t *testing.T, err error) {
	t.Helper()
	if !isTokenInvalid(err) {
		t.Fatalf("err = %v, want the invalid-token validation error", err)
	}
}

// tokenSecrets returns the raw token and its hash in the encodings a log line
// or error message could plausibly contain.
func tokenSecrets(raw string) []string {
	hash := HashToken(raw)
	return []string{raw, hex.EncodeToString(hash), base64.StdEncoding.EncodeToString(hash), fmt.Sprint(hash)}
}

func requireNoSecrets(t *testing.T, what, s string, secrets []string) {
	t.Helper()
	for _, secret := range secrets {
		if strings.Contains(s, secret) {
			t.Errorf("%s leaks a secret: %s", what, s)
		}
	}
}

// injectFailure makes every statement matching event (e.g. "BEFORE UPDATE ON
// users") raise an error, simulating a failure inside a transaction. It
// returns a function that removes the trigger; cleanup also removes it.
func injectFailure(t *testing.T, pool *pgxpool.Pool, event string) (remove func()) {
	t.Helper()
	ctx := context.Background()
	table := event[strings.LastIndex(event, " ")+1:]
	remove = func() {
		if _, err := pool.Exec(ctx, `DROP TRIGGER IF EXISTS test_injected_failure ON `+table+`;
			DROP FUNCTION IF EXISTS test_injected_failure()`); err != nil {
			t.Errorf("remove injected failure: %v", err)
		}
	}
	remove()
	_, err := pool.Exec(ctx, `
		CREATE FUNCTION test_injected_failure() RETURNS trigger LANGUAGE plpgsql
		AS $$ BEGIN RAISE EXCEPTION 'injected failure'; END $$;
		CREATE TRIGGER test_injected_failure `+event+` FOR EACH ROW EXECUTE FUNCTION test_injected_failure()`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(remove)
	return remove
}

// waitForLockWaiters blocks until n sessions are waiting on a lock, so a
// test can order concurrent transactions deterministically.
func waitForLockWaiters(t *testing.T, pool *pgxpool.Pool, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		waiting := countRows(t, pool, `SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock'`)
		if waiting >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d sessions waiting on locks, want %d", waiting, n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// lockUser starts a transaction holding the user's row lock, standing in for
// a concurrent verify or resend that got there first.
func lockUser(t *testing.T, pool *pgxpool.Pool, addr string) pgx.Tx {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	if _, err := tx.Exec(ctx, `SELECT id FROM users WHERE email = $1 FOR UPDATE`, addr); err != nil {
		t.Fatal(err)
	}
	return tx
}

// ---- Verification ----

func TestVerifyEmail(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	tok := seedUser(t, s.pool, "ana@example.com")
	before := loadUser(t, s.pool, "ana@example.com")

	if err := s.VerifyEmail(context.Background(), tok.Raw); err != nil {
		t.Fatal(err)
	}

	after := loadUser(t, s.pool, "ana@example.com")
	if after.verifiedAt == nil {
		t.Fatal("email_verified_at not set")
	}
	if !after.updatedAt.After(before.updatedAt) {
		t.Error("updated_at not advanced")
	}
	if after.passwordHash != before.passwordHash {
		t.Error("password hash changed")
	}
	if !tokenUsed(t, s.pool, tok) {
		t.Error("token not marked used")
	}
	if n := countRows(t, s.pool, `SELECT count(*) FROM sessions`); n != 0 {
		t.Errorf("sessions = %d, want 0: verification must not log in", n)
	}
	// The hash is stored by design; the raw token must not be.
	requireNoSecrets(t, "database", dumpTables(t, s.pool), []string{tok.Raw})
}

func TestVerifyEmailTokenIsSingleUse(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	tok := seedUser(t, s.pool, "ana@example.com")
	if err := s.VerifyEmail(context.Background(), tok.Raw); err != nil {
		t.Fatal(err)
	}
	first := loadUser(t, s.pool, "ana@example.com")

	requireTokenInvalid(t, s.VerifyEmail(context.Background(), tok.Raw))
	if again := loadUser(t, s.pool, "ana@example.com"); !again.verifiedAt.Equal(*first.verifiedAt) {
		t.Error("replay changed email_verified_at")
	}
}

func TestVerifyEmailRejectsExpiredToken(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	tok := seedUser(t, s.pool, "ana@example.com")
	if _, err := s.pool.Exec(context.Background(),
		`UPDATE user_tokens SET expires_at = now() - interval '1 second'`); err != nil {
		t.Fatal(err)
	}

	requireTokenInvalid(t, s.VerifyEmail(context.Background(), tok.Raw))
	if loadUser(t, s.pool, "ana@example.com").verifiedAt != nil {
		t.Error("expired token verified the email")
	}
	if tokenUsed(t, s.pool, tok) {
		t.Error("expired token marked used")
	}
}

func TestVerifyEmailRejectsUnknownToken(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	tok := seedUser(t, s.pool, "ana@example.com")

	requireTokenInvalid(t, s.VerifyEmail(context.Background(), NewToken("").Raw))
	if loadUser(t, s.pool, "ana@example.com").verifiedAt != nil || tokenUsed(t, s.pool, tok) {
		t.Error("unknown token changed state")
	}
}

func TestVerifyEmailRejectsMalformedTokenWithoutDatabase(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	s.pool.Close() // any database access would now return an error

	valid := NewToken("").Raw
	for _, raw := range []string{"", valid[:42], valid + "A", "+" + valid[1:], strings.Repeat("ab", 32), AccessTokenPrefix + valid, "ñ"} {
		requireTokenInvalid(t, s.VerifyEmail(context.Background(), raw))
	}
	// Control: a well-formed token does reach the (closed) database.
	if err := s.VerifyEmail(context.Background(), valid); err == nil || isTokenInvalid(err) {
		t.Fatalf("well-formed token: err = %v, want a database error", err)
	}
}

func TestVerifyEmailRejectsWrongPurposeToken(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	seedUser(t, s.pool, "ana@example.com")
	reset := NewToken("")
	if _, err := s.pool.Exec(context.Background(),
		`INSERT INTO user_tokens (user_id, purpose, token_hash, expires_at)
		 SELECT id, 'password_reset', $1, now() + interval '1 hour' FROM users`, reset.Hash); err != nil {
		t.Fatal(err)
	}

	requireTokenInvalid(t, s.VerifyEmail(context.Background(), reset.Raw))
	if loadUser(t, s.pool, "ana@example.com").verifiedAt != nil || tokenUsed(t, s.pool, reset) {
		t.Error("password reset token was accepted for verification")
	}
}

func TestVerifyEmailVerifiesOnlyTokenOwner(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	ana := seedUser(t, s.pool, "ana@example.com")
	seedUser(t, s.pool, "bob@example.com")

	if err := s.VerifyEmail(context.Background(), ana.Raw); err != nil {
		t.Fatal(err)
	}
	if loadUser(t, s.pool, "bob@example.com").verifiedAt != nil {
		t.Error("another user's token verified bob")
	}
}

// Only a race can leave a live token on a verified account; consuming it is
// harmless and must keep the original verification time.
func TestVerifyEmailKeepsExistingVerificationTime(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	tok := seedUser(t, s.pool, "ana@example.com")
	verifiedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if _, err := s.pool.Exec(context.Background(), `UPDATE users SET email_verified_at = $1`, verifiedAt); err != nil {
		t.Fatal(err)
	}

	if err := s.VerifyEmail(context.Background(), tok.Raw); err != nil {
		t.Fatal(err)
	}
	if got := loadUser(t, s.pool, "ana@example.com").verifiedAt; !got.Equal(verifiedAt) {
		t.Errorf("email_verified_at = %v, want unchanged %v", got, verifiedAt)
	}
	if !tokenUsed(t, s.pool, tok) {
		t.Error("token not consumed")
	}
}

// A failure after the token UPDATE must roll it back: the token stays usable.
func TestVerifyEmailFailureLeavesTokenUsable(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	tok := seedUser(t, s.pool, "ana@example.com")
	remove := injectFailure(t, s.pool, "BEFORE UPDATE ON users")

	err := s.VerifyEmail(context.Background(), tok.Raw)
	if err == nil || isTokenInvalid(err) {
		t.Fatalf("err = %v, want the injected database error", err)
	}
	requireNoSecrets(t, "error", err.Error(), tokenSecrets(tok.Raw))
	if tokenUsed(t, s.pool, tok) || loadUser(t, s.pool, "ana@example.com").verifiedAt != nil {
		t.Fatal("failed verification was partly committed")
	}

	remove()
	if err := s.VerifyEmail(context.Background(), tok.Raw); err != nil {
		t.Fatalf("token unusable after a failed attempt: %v", err)
	}
}

func TestVerifyEmailHonorsCancelledContext(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	tok := seedUser(t, s.pool, "ana@example.com")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := s.VerifyEmail(ctx, tok.Raw); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if tokenUsed(t, s.pool, tok) {
		t.Error("token consumed by a cancelled request")
	}
}

// Case 1: two requests with the same token, released at the same moment.
func TestVerifyEmailConcurrentSameTokenSucceedsOnce(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	tok := seedUser(t, s.pool, "ana@example.com")
	blocker := lockUser(t, s.pool, "ana@example.com")

	results := make(chan error, 2)
	for range 2 {
		go func() { results <- s.VerifyEmail(context.Background(), tok.Raw) }()
	}
	waitForLockWaiters(t, s.pool, 2)
	if err := blocker.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}

	requireOneSuccess(t, []error{<-results, <-results})
}

func TestVerifyEmailConcurrentStress(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	tok := seedUser(t, s.pool, "ana@example.com")

	const n = 20
	start := make(chan struct{})
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			<-start
			errs[i] = s.VerifyEmail(context.Background(), tok.Raw)
		})
	}
	close(start)
	wg.Wait()
	requireOneSuccess(t, errs)
}

func requireOneSuccess(t *testing.T, errs []error) {
	t.Helper()
	successes := 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case !isTokenInvalid(err):
			t.Errorf("unexpected error (deadlock or failure?): %v", err)
		}
	}
	if successes != 1 {
		t.Errorf("%d verifications succeeded, want exactly 1", successes)
	}
}

// ---- Resend ----

func TestResendVerificationRotatesToken(t *testing.T) {
	rec := &email.Recorder{}
	s := newTestService(t, rec)
	old := seedUser(t, s.pool, "ana@example.com")
	before := loadUser(t, s.pool, "ana@example.com")

	if err := s.ResendVerification(context.Background(), " ANA@Example.com "); err != nil {
		t.Fatal(err)
	}
	s.Wait()

	msgs := rec.Messages()
	if len(msgs) != 1 {
		t.Fatalf("sent %d emails, want 1", len(msgs))
	}
	raw := tokenIn(t, msgs[0])
	if want := verificationEmail(mustParseURL(t, testBaseURL), "ana@example.com", raw, 24*time.Hour); msgs[0] != want {
		t.Errorf("email = %#v, want the verification email", msgs[0])
	}
	if n := unusedTokens(t, s.pool, "ana@example.com"); n != 1 {
		t.Errorf("unused tokens = %d, want 1", n)
	}
	if loadUser(t, s.pool, "ana@example.com").passwordHash != before.passwordHash {
		t.Error("resend changed the password")
	}
	requireNoSecrets(t, "database", dumpTables(t, s.pool), []string{raw})

	requireTokenInvalid(t, s.VerifyEmail(context.Background(), old.Raw))
	if err := s.VerifyEmail(context.Background(), raw); err != nil {
		t.Fatalf("new token: %v", err)
	}
}

func TestResendVerificationIgnoresVerifiedAccount(t *testing.T) {
	rec := &email.Recorder{}
	s := newTestService(t, rec)
	tok := seedUser(t, s.pool, "ana@example.com")
	if err := s.VerifyEmail(context.Background(), tok.Raw); err != nil {
		t.Fatal(err)
	}
	before := dumpTables(t, s.pool)

	if err := s.ResendVerification(context.Background(), "ana@example.com"); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	if after := dumpTables(t, s.pool); after != before {
		t.Errorf("resend changed a verified account:\nbefore %s\nafter  %s", before, after)
	}
	if n := len(rec.Messages()); n != 0 {
		t.Errorf("sent %d emails, want 0", n)
	}
}

func TestResendVerificationIgnoresUnknownAddress(t *testing.T) {
	rec := &email.Recorder{}
	s := newTestService(t, rec)

	if err := s.ResendVerification(context.Background(), "nobody@example.com"); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	if n := countRows(t, s.pool, `SELECT count(*) FROM users`) + countRows(t, s.pool, `SELECT count(*) FROM user_tokens`); n != 0 {
		t.Errorf("rows written = %d, want 0", n)
	}
	if n := len(rec.Messages()); n != 0 {
		t.Errorf("sent %d emails, want 0", n)
	}
}

func TestResendVerificationRejectsInvalidEmail(t *testing.T) {
	rec := &email.Recorder{}
	s := newTestService(t, rec)

	err := s.ResendVerification(context.Background(), "not-an-email")
	var verr *ValidationError
	if !errors.As(err, &verr) || !slices.Equal(verr.Fields, []*FieldError{ErrEmailInvalid}) {
		t.Fatalf("err = %v, want invalid email", err)
	}
	s.Wait()
	if n := len(rec.Messages()); n != 0 {
		t.Errorf("sent %d emails, want 0", n)
	}
}

func TestResendVerificationSendsOnlyAfterCommit(t *testing.T) {
	var s testService
	var sent email.Message
	var visibleHash []byte
	s = newTestService(t, senderFunc(func(ctx context.Context, msg email.Message) error {
		// Another connection sees the new token only once it is committed.
		sent = msg
		return s.pool.QueryRow(ctx, `SELECT token_hash FROM user_tokens WHERE used_at IS NULL`).Scan(&visibleHash)
	}))
	seedUser(t, s.pool, "ana@example.com")

	if err := s.ResendVerification(context.Background(), "ana@example.com"); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	if !slices.Equal(visibleHash, HashToken(tokenIn(t, sent))) {
		t.Error("email sent before the new token was committed")
	}
}

func TestResendVerificationEmailFailure(t *testing.T) {
	var sent email.Message
	s := newTestService(t, senderFunc(func(ctx context.Context, msg email.Message) error {
		sent = msg
		return errors.New("provider unavailable")
	}))
	seedUser(t, s.pool, "ana@example.com")

	if err := s.ResendVerification(context.Background(), "ana@example.com"); err != nil {
		t.Fatalf("err = %v, want nil: delivery failure must not change the result", err)
	}
	s.Wait()

	raw := tokenIn(t, sent)
	if !tokenUsable(t, s.pool, raw) {
		t.Error("new token not stored; the user could not verify with a later resend's link either")
	}
	logs := s.logs.String()
	if !strings.Contains(logs, "provider unavailable") {
		t.Errorf("failure not logged: %s", logs)
	}
	requireNoSecrets(t, "logs", logs, append(tokenSecrets(raw), "ana@example.com"))
}

// tokenUsable reports whether raw's hash is stored as an unused token.
func tokenUsable(t *testing.T, pool *pgxpool.Pool, raw string) bool {
	t.Helper()
	return countRows(t, pool, `SELECT count(*) FROM user_tokens WHERE token_hash = $1 AND used_at IS NULL`, HashToken(raw)) == 1
}

// A failure after the old token was deleted must roll the delete back.
func TestResendVerificationFailureKeepsOldToken(t *testing.T) {
	rec := &email.Recorder{}
	s := newTestService(t, rec)
	old := seedUser(t, s.pool, "ana@example.com")
	injectFailure(t, s.pool, "BEFORE INSERT ON user_tokens")

	err := s.ResendVerification(context.Background(), "ana@example.com")
	var verr *ValidationError
	if err == nil || errors.As(err, &verr) {
		t.Fatalf("err = %v, want the injected database error", err)
	}
	if strings.Contains(err.Error(), "ana@example.com") {
		t.Errorf("error leaks the address: %v", err)
	}
	s.Wait()
	if n := len(rec.Messages()); n != 0 {
		t.Errorf("sent %d emails after a failed resend, want 0", n)
	}
	if !tokenUsable(t, s.pool, old.Raw) {
		t.Error("old token lost although the resend failed")
	}
}

func TestResendVerificationHonorsCancelledContext(t *testing.T) {
	rec := &email.Recorder{}
	s := newTestService(t, rec)
	old := seedUser(t, s.pool, "ana@example.com")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := s.ResendVerification(ctx, "ana@example.com"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	s.Wait()
	if !tokenUsable(t, s.pool, old.Raw) || len(rec.Messages()) != 0 {
		t.Error("cancelled resend changed state or sent email")
	}
}

// Case 3: two resends at once. They serialize on the user row, both
// succeed, and only the later token stays usable.
func TestConcurrentResendsLeaveOneActiveToken(t *testing.T) {
	rec := &email.Recorder{}
	s := newTestService(t, rec)
	seedUser(t, s.pool, "ana@example.com")
	blocker := lockUser(t, s.pool, "ana@example.com")

	results := make(chan error, 2)
	for range 2 {
		go func() { results <- s.ResendVerification(context.Background(), "ana@example.com") }()
	}
	waitForLockWaiters(t, s.pool, 2)
	if err := blocker.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("concurrent resend failed: %v", err)
		}
	}
	s.Wait()

	if n := unusedTokens(t, s.pool, "ana@example.com"); n != 1 {
		t.Fatalf("unused tokens = %d, want 1", n)
	}
	msgs := rec.Messages()
	if len(msgs) != 2 {
		t.Fatalf("sent %d emails, want 2", len(msgs))
	}
	usable := 0
	for _, msg := range msgs {
		if tokenUsable(t, s.pool, tokenIn(t, msg)) {
			usable++
		}
	}
	if usable != 1 {
		t.Errorf("%d emailed tokens usable, want exactly 1", usable)
	}
}

// Cases 2 and 4, resend first: verify waits for the resend holding the user
// lock, then finds its token rotated away. With the opposite lock order
// (token row before user row) this interleaving deadlocks.
func TestVerifyWaitingForResendDoesNotDeadlock(t *testing.T) {
	s := newTestService(t, &email.Recorder{})
	old := seedUser(t, s.pool, "ana@example.com")
	resend := lockUser(t, s.pool, "ana@example.com")

	result := make(chan error, 1)
	go func() { result <- s.VerifyEmail(context.Background(), old.Raw) }()
	waitForLockWaiters(t, s.pool, 1)

	// The resend now deletes the old token that verify is after.
	fresh := NewToken("")
	issued, err := reissueVerificationTokenTx(context.Background(), resend, "ana@example.com", fresh.Hash, verificationTokenTTL)
	if err != nil || !issued {
		t.Fatalf("resend in transaction: issued=%v err=%v", issued, err)
	}
	if err := resend.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}

	requireTokenInvalid(t, <-result)
	if loadUser(t, s.pool, "ana@example.com").verifiedAt != nil {
		t.Error("stale token verified the email after rotation")
	}
	if err := s.VerifyEmail(context.Background(), fresh.Raw); err != nil {
		t.Fatalf("new token: %v", err)
	}
}

// Case 2, verify first: the resend waits, then sees the account verified and
// issues nothing.
func TestResendWaitingForVerificationIssuesNothing(t *testing.T) {
	rec := &email.Recorder{}
	s := newTestService(t, rec)
	tok := seedUser(t, s.pool, "ana@example.com")
	verify := lockUser(t, s.pool, "ana@example.com")
	ok, err := consumeVerificationTokenTx(context.Background(), verify, tok.Hash)
	if err != nil || !ok {
		t.Fatalf("verify in transaction: ok=%v err=%v", ok, err)
	}

	result := make(chan error, 1)
	go func() { result <- s.ResendVerification(context.Background(), "ana@example.com") }()
	waitForLockWaiters(t, s.pool, 1)
	if err := verify.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := <-result; err != nil {
		t.Fatal(err)
	}
	s.Wait()
	if n := unusedTokens(t, s.pool, "ana@example.com"); n != 0 {
		t.Errorf("verified account got %d new tokens", n)
	}
	if n := len(rec.Messages()); n != 0 {
		t.Errorf("verified account was sent %d emails", n)
	}
}

// Verify and resend released together, many times: no errors, and every
// outcome is one of the two consistent end states.
func TestVerifyAndResendRace(t *testing.T) {
	rec := &email.Recorder{}
	s := newTestService(t, rec)

	for i := range 30 {
		addr := fmt.Sprintf("user%d@example.com", i)
		old := seedUser(t, s.pool, addr)
		sentBefore := len(rec.Messages())

		start := make(chan struct{})
		var verifyErr, resendErr error
		var wg sync.WaitGroup
		wg.Go(func() { <-start; verifyErr = s.VerifyEmail(context.Background(), old.Raw) })
		wg.Go(func() { <-start; resendErr = s.ResendVerification(context.Background(), addr) })
		close(start)
		wg.Wait()
		s.Wait()

		if resendErr != nil || (verifyErr != nil && !isTokenInvalid(verifyErr)) {
			t.Fatalf("iteration %d: verify err = %v, resend err = %v", i, verifyErr, resendErr)
		}
		verified := loadUser(t, s.pool, addr).verifiedAt != nil
		sent := len(rec.Messages()) - sentBefore
		unused := unusedTokens(t, s.pool, addr)
		switch {
		case verifyErr == nil && verified && sent == 0 && unused == 0:
			// Verify won; resend saw a verified account.
		case isTokenInvalid(verifyErr) && !verified && sent == 1 && unused == 1:
			// Resend won; the old token was rotated away.
		default:
			t.Fatalf("iteration %d: inconsistent state: verifyErr=%v verified=%v sent=%d unused=%d",
				i, verifyErr, verified, sent, unused)
		}
	}
}
