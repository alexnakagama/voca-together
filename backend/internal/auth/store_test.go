package auth

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"vocatogether/backend/internal/testutil"
)

func countRows(t *testing.T, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCreateUserWithVerificationToken(t *testing.T) {
	pool := testutil.DB(t)
	tok := NewToken("")

	created, err := createUserWithVerificationToken(context.Background(), pool,
		"ana@example.com", "hash", tok.Hash, verificationTokenTTL)
	if err != nil || !created {
		t.Fatalf("created = %v, err = %v; want true, nil", created, err)
	}

	var purpose string
	var tokenHash []byte
	var ttlSeconds float64
	var unused bool
	err = pool.QueryRow(context.Background(),
		`SELECT t.purpose, t.token_hash, EXTRACT(EPOCH FROM t.expires_at - t.created_at), t.used_at IS NULL
		 FROM user_tokens t JOIN users u ON u.id = t.user_id WHERE u.email = 'ana@example.com'`).
		Scan(&purpose, &tokenHash, &ttlSeconds, &unused)
	if err != nil {
		t.Fatal(err)
	}
	if purpose != PurposeEmailVerification || string(tokenHash) != string(tok.Hash) || !unused {
		t.Errorf("token row = (%q, %x, unused=%v)", purpose, tokenHash, unused)
	}
	// Same transaction, so now() is identical for created_at and expires_at.
	if ttlSeconds != 24*60*60 {
		t.Errorf("expires_at - created_at = %vs, want 24h", ttlSeconds)
	}
}

func TestCreateUserWithExistingEmailWritesNothing(t *testing.T) {
	pool := testutil.DB(t)
	ctx := context.Background()
	if _, err := createUserWithVerificationToken(ctx, pool, "ana@example.com", "first", NewToken("").Hash, verificationTokenTTL); err != nil {
		t.Fatal(err)
	}

	created, err := createUserWithVerificationToken(ctx, pool, "ana@example.com", "second", NewToken("").Hash, verificationTokenTTL)
	if err != nil || created {
		t.Fatalf("created = %v, err = %v; want false, nil", created, err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM users WHERE password_hash = 'first'`); n != 1 {
		t.Errorf("original user rows = %d, want 1 (password must not be overwritten)", n)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM user_tokens`); n != 1 {
		t.Errorf("tokens = %d, want 1 (no token for an existing account)", n)
	}
}

// A failure after the user insert must roll the user back too.
func TestCreateUserRollsBackWhenTokenInsertFails(t *testing.T) {
	pool := testutil.DB(t)
	ctx := context.Background()
	tok := NewToken("")
	if _, err := createUserWithVerificationToken(ctx, pool, "ana@example.com", "hash", tok.Hash, verificationTokenTTL); err != nil {
		t.Fatal(err)
	}

	// Reusing the token hash violates user_tokens_token_hash_key.
	created, err := createUserWithVerificationToken(ctx, pool, "bob@example.com", "hash", tok.Hash, verificationTokenTTL)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.ConstraintName != "user_tokens_token_hash_key" {
		t.Fatalf("err = %v, want unique violation on user_tokens_token_hash_key", err)
	}
	if created {
		t.Error("created = true on failure")
	}
	if strings.Contains(err.Error(), "bob@example.com") {
		t.Errorf("error leaks the email address: %v", err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM users WHERE email = 'bob@example.com'`); n != 0 {
		t.Errorf("user without token was committed (rows = %d)", n)
	}
}

func TestCreateUserHonorsCancelledContext(t *testing.T) {
	pool := testutil.DB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := createUserWithVerificationToken(ctx, pool, "ana@example.com", "hash", NewToken("").Hash, verificationTokenTTL)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM users`); n != 0 {
		t.Errorf("users = %d, want 0", n)
	}
}
