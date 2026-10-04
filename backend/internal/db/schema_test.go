package db_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"vocatogether/backend/internal/testutil"
)

const (
	uniqueViolation = "23505"
	checkViolation  = "23514"
)

func requirePgError(t *testing.T, err error, code, constraint string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("expected PostgreSQL error %s (%s), got %v", code, constraint, err)
	}
	if pgErr.Code != code || pgErr.ConstraintName != constraint {
		t.Fatalf("got error %s on %q, want %s on %q", pgErr.Code, pgErr.ConstraintName, code, constraint)
	}
}

func insertUser(t *testing.T, pool *pgxpool.Pool, email string) (string, error) {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(),
		`INSERT INTO users (email, password_hash) VALUES ($1, 'x') RETURNING id`, email).Scan(&id)
	return id, err
}

func mustInsertUser(t *testing.T, pool *pgxpool.Pool, email string) string {
	t.Helper()
	id, err := insertUser(t, pool, email)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}

func hash(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }

func insertToken(pool *pgxpool.Pool, userID, purpose string, tokenHash []byte) error {
	_, err := pool.Exec(context.Background(),
		`INSERT INTO user_tokens (user_id, purpose, token_hash, expires_at) VALUES ($1, $2, $3, $4)`,
		userID, purpose, tokenHash, time.Now().Add(time.Hour))
	return err
}

func insertSession(pool *pgxpool.Pool, userID string, access, refresh []byte) error {
	now := time.Now()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO sessions (user_id, access_token_hash, access_expires_at, refresh_token_hash, refresh_expires_at, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		userID, access, now.Add(15*time.Minute), refresh, now.Add(30*24*time.Hour), now.Add(90*24*time.Hour))
	return err
}

func TestUsersDefaults(t *testing.T) {
	pool := testutil.DB(t)
	id := mustInsertUser(t, pool, "ana@example.com")

	var verifiedAt *time.Time
	var createdAt, updatedAt time.Time
	err := pool.QueryRow(context.Background(),
		`SELECT email_verified_at, created_at, updated_at FROM users WHERE id = $1`, id).
		Scan(&verifiedAt, &createdAt, &updatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if verifiedAt != nil {
		t.Error("new users must start unverified")
	}
	if createdAt.IsZero() || updatedAt.IsZero() {
		t.Error("timestamps must default to now()")
	}
}

func TestUsersEmailUnique(t *testing.T) {
	pool := testutil.DB(t)
	mustInsertUser(t, pool, "ana@example.com")
	_, err := insertUser(t, pool, "ana@example.com")
	requirePgError(t, err, uniqueViolation, "users_email_key")
}

// Emails must be stored normalized; together with the unique constraint this makes uniqueness case-insensitive.
func TestUsersEmailMustBeLowercase(t *testing.T) {
	pool := testutil.DB(t)
	_, err := insertUser(t, pool, "Ana@Example.com")
	requirePgError(t, err, checkViolation, "users_email_lower")
}

func TestUsersEmailLength(t *testing.T) {
	pool := testutil.DB(t)
	_, err := insertUser(t, pool, "a@")
	requirePgError(t, err, checkViolation, "users_email_length")

	long := strings.Repeat("a", 243) + "@example.com" // 255 chars
	_, err = insertUser(t, pool, long)
	requirePgError(t, err, checkViolation, "users_email_length")

	mustInsertUser(t, pool, strings.Repeat("a", 242)+"@example.com") // 254 chars is allowed
}

func TestUserTokensPurposeRestricted(t *testing.T) {
	pool := testutil.DB(t)
	uid := mustInsertUser(t, pool, "ana@example.com")
	err := insertToken(pool, uid, "magic_login", hash(1))
	requirePgError(t, err, checkViolation, "user_tokens_purpose_check")
}

func TestUserTokensHashMustBe32Bytes(t *testing.T) {
	pool := testutil.DB(t)
	uid := mustInsertUser(t, pool, "ana@example.com")
	err := insertToken(pool, uid, "email_verification", []byte("too-short"))
	requirePgError(t, err, checkViolation, "user_tokens_token_hash_length")
}

func TestUserTokensHashUnique(t *testing.T) {
	pool := testutil.DB(t)
	a := mustInsertUser(t, pool, "ana@example.com")
	b := mustInsertUser(t, pool, "ben@example.com")
	if err := insertToken(pool, a, "email_verification", hash(1)); err != nil {
		t.Fatal(err)
	}
	err := insertToken(pool, b, "password_reset", hash(1))
	requirePgError(t, err, uniqueViolation, "user_tokens_token_hash_key")
}

func TestUserTokensOneActivePerUserAndPurpose(t *testing.T) {
	ctx := context.Background()
	pool := testutil.DB(t)
	uid := mustInsertUser(t, pool, "ana@example.com")

	if err := insertToken(pool, uid, "email_verification", hash(1)); err != nil {
		t.Fatal(err)
	}
	// A second unused token for the same purpose is rejected...
	err := insertToken(pool, uid, "email_verification", hash(2))
	requirePgError(t, err, uniqueViolation, "user_tokens_one_active")

	// ...but a different purpose is independent...
	if err := insertToken(pool, uid, "password_reset", hash(3)); err != nil {
		t.Fatalf("different purpose should be allowed: %v", err)
	}

	// ...and once the first token is used, a new one can be issued.
	if _, err := pool.Exec(ctx, `UPDATE user_tokens SET used_at = now() WHERE token_hash = $1`, hash(1)); err != nil {
		t.Fatal(err)
	}
	if err := insertToken(pool, uid, "email_verification", hash(4)); err != nil {
		t.Fatalf("new token after the old one was used should be allowed: %v", err)
	}
}

func TestSessionsTokenHashesUnique(t *testing.T) {
	pool := testutil.DB(t)
	uid := mustInsertUser(t, pool, "ana@example.com")
	if err := insertSession(pool, uid, hash(1), hash(2)); err != nil {
		t.Fatal(err)
	}
	requirePgError(t, insertSession(pool, uid, hash(1), hash(3)), uniqueViolation, "sessions_access_token_hash_key")
	requirePgError(t, insertSession(pool, uid, hash(4), hash(2)), uniqueViolation, "sessions_refresh_token_hash_key")
}

func TestSessionsTokenHashesMustBe32Bytes(t *testing.T) {
	pool := testutil.DB(t)
	uid := mustInsertUser(t, pool, "ana@example.com")
	requirePgError(t, insertSession(pool, uid, []byte("x"), hash(2)), checkViolation, "sessions_access_token_hash_length")
	requirePgError(t, insertSession(pool, uid, hash(1), []byte("x")), checkViolation, "sessions_refresh_token_hash_length")
}

func TestDeletingUserCascades(t *testing.T) {
	ctx := context.Background()
	pool := testutil.DB(t)
	uid := mustInsertUser(t, pool, "ana@example.com")
	if err := insertToken(pool, uid, "email_verification", hash(1)); err != nil {
		t.Fatal(err)
	}
	if err := insertSession(pool, uid, hash(2), hash(3)); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, uid); err != nil {
		t.Fatal(err)
	}

	var tokens, sessions int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM user_tokens), (SELECT count(*) FROM sessions)`).Scan(&tokens, &sessions); err != nil {
		t.Fatal(err)
	}
	if tokens != 0 || sessions != 0 {
		t.Errorf("after deleting the user: %d tokens, %d sessions remain; want 0", tokens, sessions)
	}
}

func TestSessionsUserAgentLength(t *testing.T) {
	ctx := context.Background()
	pool := testutil.DB(t)
	uid := mustInsertUser(t, pool, "ana@example.com")
	insert := func(access, refresh []byte, ua string) error {
		_, err := pool.Exec(ctx,
			`INSERT INTO sessions (user_id, access_token_hash, access_expires_at, refresh_token_hash, refresh_expires_at, expires_at, user_agent)
			 VALUES ($1, $2, now() + interval '15 minutes', $3, now() + interval '30 days', now() + interval '90 days', $4)`,
			uid, access, refresh, ua)
		return err
	}
	if err := insert(hash(1), hash(2), strings.Repeat("a", 256)); err != nil {
		t.Fatalf("256-byte user agent rejected: %v", err)
	}
	// The limit counts bytes, not characters: 128 two-byte runes plus one byte is 257.
	requirePgError(t, insert(hash(3), hash(4), strings.Repeat("ñ", 128)+"a"), checkViolation, "sessions_user_agent_length")
}

func TestSessionsExpiriesWithinAbsoluteLifetime(t *testing.T) {
	ctx := context.Background()
	pool := testutil.DB(t)
	uid := mustInsertUser(t, pool, "ana@example.com")
	insert := func(access, refresh []byte, accessTTL, refreshTTL string) error {
		_, err := pool.Exec(ctx,
			`INSERT INTO sessions (user_id, access_token_hash, access_expires_at, refresh_token_hash, refresh_expires_at, expires_at)
			 VALUES ($1, $2, now() + $3::interval, $4, now() + $5::interval, now() + interval '90 days')`,
			uid, access, accessTTL, refresh, refreshTTL)
		return err
	}
	if err := insert(hash(1), hash(2), "90 days", "90 days"); err != nil {
		t.Fatalf("expiries equal to the absolute lifetime rejected: %v", err)
	}
	requirePgError(t, insert(hash(3), hash(4), "91 days", "30 days"), checkViolation, "sessions_expiry_order")
	requirePgError(t, insert(hash(5), hash(6), "15 minutes", "91 days"), checkViolation, "sessions_expiry_order")
}

const foreignKeyViolation = "23503"

// insertGoogleUser creates a passwordless user and its Google identity in one
// transaction, the only way such a user can be committed (decision 020).
func insertGoogleUser(t *testing.T, pool *pgxpool.Pool, email, subject string) string {
	t.Helper()
	ctx := context.Background()
	var id string
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`INSERT INTO users (email, password_hash, email_verified_at) VALUES ($1, NULL, now()) RETURNING id`,
			email).Scan(&id); err != nil {
			return err
		}
		return insertIdentity(ctx, tx, id, "google", subject)
	})
	if err != nil {
		t.Fatalf("insert google user: %v", err)
	}
	return id
}

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func insertIdentity(ctx context.Context, db execer, userID, provider, subject string) error {
	_, err := db.Exec(ctx,
		`INSERT INTO user_identities (user_id, provider, subject) VALUES ($1, $2, $3)`, userID, provider, subject)
	return err
}

func TestUsersPasswordHashMayBeNullButNotEmpty(t *testing.T) {
	ctx := context.Background()
	pool := testutil.DB(t)
	id := insertGoogleUser(t, pool, "ana@example.com", "1001")

	var hash *string
	if err := pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, id).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash != nil {
		t.Errorf("password_hash = %q, want NULL", *hash)
	}

	_, err := pool.Exec(ctx, `INSERT INTO users (email, password_hash) VALUES ('ben@example.com', '')`)
	requirePgError(t, err, checkViolation, "users_password_hash_not_empty")
}

// Every user needs a password or an identity; the check runs at commit, so a
// user and its identity can be inserted in either order within a transaction.
func TestUsersRequireAnAuthenticationMethod(t *testing.T) {
	ctx := context.Background()
	pool := testutil.DB(t)

	_, err := pool.Exec(ctx, `INSERT INTO users (email, password_hash) VALUES ('ana@example.com', NULL)`)
	requirePgError(t, err, checkViolation, "users_auth_method_required")

	google := insertGoogleUser(t, pool, "google@example.com", "1001")
	password := mustInsertUser(t, pool, "pw@example.com")

	// Removing the only method fails, whichever table it is removed from.
	_, err = pool.Exec(ctx, `DELETE FROM user_identities WHERE user_id = $1`, google)
	requirePgError(t, err, checkViolation, "users_auth_method_required")
	_, err = pool.Exec(ctx, `UPDATE users SET password_hash = NULL WHERE id = $1`, password)
	requirePgError(t, err, checkViolation, "users_auth_method_required")
	_, err = pool.Exec(ctx, `UPDATE user_identities SET user_id = $2 WHERE user_id = $1`, google, password)
	requirePgError(t, err, checkViolation, "users_auth_method_required")

	// With a second method, either one can go.
	if err := insertIdentity(ctx, pool, password, "google", "1002"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET password_hash = NULL WHERE id = $1`, password); err != nil {
		t.Errorf("removing the password of a user with an identity: %v", err)
	}

	// Deleting the user takes its identities with it; that is allowed.
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, google); err != nil {
		t.Errorf("deleting a passwordless user: %v", err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM user_identities WHERE user_id = $1`, google); n != 0 {
		t.Errorf("identities of a deleted user = %d, want 0", n)
	}
}

func TestUserIdentitiesConstraints(t *testing.T) {
	ctx := context.Background()
	pool := testutil.DB(t)
	ana := insertGoogleUser(t, pool, "ana@example.com", "1001")
	ben := mustInsertUser(t, pool, "ben@example.com")

	// One identity has one owner.
	requirePgError(t, insertIdentity(ctx, pool, ben, "google", "1001"), uniqueViolation, "user_identities_provider_subject_key")
	// One Google identity per user.
	requirePgError(t, insertIdentity(ctx, pool, ana, "google", "1002"), uniqueViolation, "user_identities_user_provider_key")

	requirePgError(t, insertIdentity(ctx, pool, ben, "facebook", "1003"), checkViolation, "user_identities_provider_check")
	requirePgError(t, insertIdentity(ctx, pool, ben, "google", ""), checkViolation, "user_identities_subject_length")
	requirePgError(t, insertIdentity(ctx, pool, ben, "google", strings.Repeat("1", 256)), checkViolation, "user_identities_subject_length")
	if err := insertIdentity(ctx, pool, ben, "google", strings.Repeat("1", 255)); err != nil {
		t.Errorf("255-byte subject rejected: %v", err)
	}
	requirePgError(t, insertIdentity(ctx, pool, "00000000-0000-0000-0000-000000000000", "google", "1004"),
		foreignKeyViolation, "user_identities_user_id_fkey")
}

func countRows(t *testing.T, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
