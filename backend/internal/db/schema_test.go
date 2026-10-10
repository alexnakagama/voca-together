package db_test

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"vocatogether/backend/internal/db"
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

const (
	foreignKeyViolation = "23503"
	notNullViolation    = "23502"
)

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

func insertProfile(pool *pgxpool.Pool, userID, displayName, bio string) error {
	_, err := pool.Exec(context.Background(),
		`INSERT INTO profiles (user_id, display_name, bio) VALUES ($1, $2, $3)`, userID, displayName, bio)
	return err
}

func TestProfilesDefaults(t *testing.T) {
	ctx := context.Background()
	pool := testutil.DB(t)
	uid := mustInsertUser(t, pool, "ana@example.com")
	if _, err := pool.Exec(ctx, `INSERT INTO profiles (user_id, display_name) VALUES ($1, 'Ana')`, uid); err != nil {
		t.Fatal(err)
	}

	var bio *string
	var createdAt, updatedAt time.Time
	err := pool.QueryRow(ctx, `SELECT bio, created_at, updated_at FROM profiles WHERE user_id = $1`, uid).
		Scan(&bio, &createdAt, &updatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if bio == nil || *bio != "" {
		t.Errorf("bio = %v, want the empty string (never NULL)", bio)
	}
	if createdAt.IsZero() || updatedAt.IsZero() {
		t.Error("timestamps must default to now()")
	}

	_, err = pool.Exec(ctx, `UPDATE profiles SET bio = NULL WHERE user_id = $1`, uid)
	requirePgError(t, err, notNullViolation, "")
}

// The primary key is the user: a second profile for the same user is refused.
func TestProfilesOnePerUser(t *testing.T) {
	pool := testutil.DB(t)
	uid := mustInsertUser(t, pool, "ana@example.com")
	if err := insertProfile(pool, uid, "Ana", ""); err != nil {
		t.Fatal(err)
	}
	requirePgError(t, insertProfile(pool, uid, "Ana again", ""), uniqueViolation, "profiles_pkey")
}

func TestProfilesDisplayNameConstraints(t *testing.T) {
	pool := testutil.DB(t)
	uid := mustInsertUser(t, pool, "ana@example.com")

	requirePgError(t, insertProfile(pool, uid, "", ""), checkViolation, "profiles_display_name_length")
	requirePgError(t, insertProfile(pool, uid, strings.Repeat("a", 51), ""), checkViolation, "profiles_display_name_length")
	requirePgError(t, insertProfile(pool, uid, " Ana", ""), checkViolation, "profiles_display_name_trimmed")
	requirePgError(t, insertProfile(pool, uid, "Ana ", ""), checkViolation, "profiles_display_name_trimmed")

	// The limit counts characters, not bytes: 50 three-byte characters fit.
	if err := insertProfile(pool, uid, strings.Repeat("あ", 50), ""); err != nil {
		t.Errorf("50-character name rejected: %v", err)
	}
}

func TestProfilesBioLength(t *testing.T) {
	pool := testutil.DB(t)
	ana := mustInsertUser(t, pool, "ana@example.com")
	ben := mustInsertUser(t, pool, "ben@example.com")

	requirePgError(t, insertProfile(pool, ana, "Ana", strings.Repeat("a", 501)), checkViolation, "profiles_bio_length")
	if err := insertProfile(pool, ben, "Ben", strings.Repeat("あ", 500)); err != nil {
		t.Errorf("500-character bio rejected: %v", err)
	}
}

func TestProfilesBelongToAnExistingUser(t *testing.T) {
	ctx := context.Background()
	pool := testutil.DB(t)
	requirePgError(t, insertProfile(pool, "00000000-0000-0000-0000-000000000000", "Nobody", ""),
		foreignKeyViolation, "profiles_user_id_fkey")

	uid := mustInsertUser(t, pool, "ana@example.com")
	if err := insertProfile(pool, uid, "Ana", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, uid); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM profiles`); n != 0 {
		t.Errorf("profiles of a deleted user = %d, want 0", n)
	}
}

func insertLanguage(ctx context.Context, db execer, code, name, endonym string) error {
	_, err := db.Exec(ctx, `INSERT INTO languages (code, name, endonym) VALUES ($1, $2, $3)`, code, name, endonym)
	return err
}

func insertUserLanguage(pool *pgxpool.Pool, userID, code, kind string, level, position int) error {
	_, err := pool.Exec(context.Background(),
		`INSERT INTO user_languages (user_id, language_code, kind, level, position) VALUES ($1, $2, $3, $4, $5)`,
		userID, code, kind, level, position)
	return err
}

// The catalog comes with the schema: testutil.DB never empties it.
func TestLanguagesCatalogIsSeeded(t *testing.T) {
	ctx := context.Background()
	pool := testutil.DB(t)

	if n := countRows(t, pool, `SELECT count(*) FROM languages`); n < 90 {
		t.Errorf("catalog has %d languages, want about a hundred", n)
	}
	for code, want := range map[string][2]string{
		"en":  {"English", "English"},
		"es":  {"Spanish", "Español"},
		"ja":  {"Japanese", "日本語"},
		"ar":  {"Arabic", "العربية"},
		"zh":  {"Chinese (Mandarin)", "中文"},
		"yue": {"Cantonese", "粵語"},
		"fil": {"Filipino (Tagalog)", "Filipino"},
	} {
		var name, endonym string
		err := pool.QueryRow(ctx, `SELECT name, endonym FROM languages WHERE code = $1`, code).Scan(&name, &endonym)
		if err != nil {
			t.Errorf("language %s: %v", code, err)
			continue
		}
		if name != want[0] || endonym != want[1] {
			t.Errorf("language %s = %q, %q; want %q, %q", code, name, endonym, want[0], want[1])
		}
	}

	// A two-letter code exists for these, so the three-letter one must not
	// be used: one language, one code.
	if n := countRows(t, pool, `SELECT count(*) FROM languages WHERE code IN ('eng', 'spa', 'zho', 'cmn', 'jpn', 'tgl')`); n != 0 {
		t.Errorf("%d languages use a three-letter code although a two-letter one exists", n)
	}
	// Endonyms are not unique by constraint (two languages may share a
	// name for themselves), but none in the seed list should be the name of
	// another language.
	if n := countRows(t, pool, `SELECT count(*) FROM languages a JOIN languages b ON a.endonym = b.name AND a.code <> b.code`); n != 0 {
		t.Errorf("%d endonyms are another language's English name", n)
	}
}

func TestLanguagesConstraints(t *testing.T) {
	ctx := context.Background()
	pool := testutil.DB(t)
	before := countRows(t, pool, `SELECT count(*) FROM languages`)

	for _, code := range []string{"", "x", "abcd", "EN", "En", "e1", "pt-BR", "zh_Hant", " en", "en ", "en\n"} {
		requirePgError(t, insertLanguage(ctx, pool, code, "Test language", "Test"), checkViolation, "languages_code_format")
	}
	requirePgError(t, insertLanguage(ctx, pool, "es", "Other Spanish", "Otro"), uniqueViolation, "languages_pkey")
	requirePgError(t, insertLanguage(ctx, pool, "zzz", "Spanish", "Otro"), uniqueViolation, "languages_name_key")

	for _, name := range []string{"", " Test", "Test ", strings.Repeat("a", 61)} {
		requirePgError(t, insertLanguage(ctx, pool, "zzz", name, "Test"), checkViolation, "languages_name_text")
		requirePgError(t, insertLanguage(ctx, pool, "zzz", "Test language", name), checkViolation, "languages_endonym_text")
	}
	_, err := pool.Exec(ctx, `INSERT INTO languages (code, name) VALUES ('zzz', 'Test language')`)
	requirePgError(t, err, notNullViolation, "")

	// What the rules allow: a three-letter code and 60 characters, counted
	// as characters. Rolled back, because the catalog is shared by every
	// test.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := insertLanguage(ctx, tx, "zzz", strings.Repeat("a", 60), strings.Repeat("あ", 60)); err != nil {
		t.Errorf("valid language rejected: %v", err)
	}
	var createdAt time.Time
	if err := tx.QueryRow(ctx, `SELECT created_at FROM languages WHERE code = 'zzz'`).Scan(&createdAt); err != nil || createdAt.IsZero() {
		t.Errorf("created_at = %v (%v), want now()", createdAt, err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	if after := countRows(t, pool, `SELECT count(*) FROM languages`); after != before {
		t.Errorf("catalog changed from %d to %d languages", before, after)
	}
}

func TestUserLanguagesConstraints(t *testing.T) {
	ctx := context.Background()
	pool := testutil.DB(t)
	ana := mustInsertUser(t, pool, "ana@example.com")
	ben := mustInsertUser(t, pool, "ben@example.com")

	requirePgError(t, insertUserLanguage(pool, ana, "es", "native", 7, 0), checkViolation, "user_languages_kind_check")
	requirePgError(t, insertUserLanguage(pool, ana, "es", "", 7, 0), checkViolation, "user_languages_kind_check")
	requirePgError(t, insertUserLanguage(pool, ana, "es", "spoken", 0, 0), checkViolation, "user_languages_level_range")
	requirePgError(t, insertUserLanguage(pool, ana, "es", "spoken", 8, 0), checkViolation, "user_languages_level_range")
	requirePgError(t, insertUserLanguage(pool, ana, "es", "learning", 7, 0), checkViolation, "user_languages_native_is_spoken")
	requirePgError(t, insertUserLanguage(pool, ana, "es", "spoken", 7, -1), checkViolation, "user_languages_position_range")
	requirePgError(t, insertUserLanguage(pool, ana, "es", "spoken", 7, 5), checkViolation, "user_languages_position_range")
	for _, column := range []string{"kind", "level", "position"} {
		_, err := pool.Exec(ctx, `INSERT INTO user_languages (user_id, language_code, kind, level, position)
			VALUES ($1, 'es', 'spoken', 7, 0)
			ON CONFLICT DO NOTHING`, ana)
		if err != nil {
			t.Fatal(err)
		}
		_, err = pool.Exec(ctx, `UPDATE user_languages SET `+column+` = NULL WHERE user_id = $1`, ana)
		requirePgError(t, err, notNullViolation, "")
	}
	if n := countRows(t, pool, `SELECT count(*) FROM user_languages WHERE user_id = $1`, ana); n != 1 {
		t.Fatalf("ana has %d languages, want the one valid row", n)
	}

	// Every level of each kind, native only when spoken: ana speaks es
	// natively (above) and five more at most.
	requirePgError(t, insertUserLanguage(pool, ana, "es", "learning", 3, 0), uniqueViolation, "user_languages_pkey")
	requirePgError(t, insertUserLanguage(pool, ana, "en", "spoken", 5, 0), uniqueViolation, "user_languages_position_key")
	for i, code := range []string{"en", "fr", "de", "it"} {
		if err := insertUserLanguage(pool, ana, code, "spoken", i+1, i+1); err != nil {
			t.Errorf("spoken %s at level %d: %v", code, i+1, err)
		}
	}
	// The same positions are free in the other kind, and for another user.
	for i, code := range []string{"ja", "ko", "zh", "yue", "fil"} {
		if err := insertUserLanguage(pool, ana, code, "learning", i+2, i); err != nil {
			t.Errorf("learning %s at level %d: %v", code, i+2, err)
		}
	}
	if err := insertUserLanguage(pool, ben, "es", "learning", 1, 0); err != nil {
		t.Errorf("another user with the same language and position: %v", err)
	}

	// Both lists are full: positions 0 to 4 are the only ones, so a sixth
	// language of a kind has nowhere to go.
	for position := range 5 {
		requirePgError(t, insertUserLanguage(pool, ana, "pt", "spoken", 4, position), uniqueViolation, "user_languages_position_key")
		requirePgError(t, insertUserLanguage(pool, ana, "pt", "learning", 4, position), uniqueViolation, "user_languages_position_key")
	}
	requirePgError(t, insertUserLanguage(pool, ana, "pt", "spoken", 4, 5), checkViolation, "user_languages_position_range")

	var createdAt time.Time
	if err := pool.QueryRow(ctx, `SELECT created_at FROM user_languages WHERE user_id = $1 AND language_code = 'es'`, ana).Scan(&createdAt); err != nil || createdAt.IsZero() {
		t.Errorf("created_at = %v (%v), want now()", createdAt, err)
	}
}

func TestUserLanguagesBelongToAnExistingUserAndLanguage(t *testing.T) {
	ctx := context.Background()
	pool := testutil.DB(t)
	requirePgError(t, insertUserLanguage(pool, "00000000-0000-0000-0000-000000000000", "es", "spoken", 7, 0),
		foreignKeyViolation, "user_languages_user_id_fkey")

	ana := mustInsertUser(t, pool, "ana@example.com")
	ben := mustInsertUser(t, pool, "ben@example.com")
	// Well-formed, but not in the catalog; and a name is not a code.
	for _, code := range []string{"zzz", "Spanish", "ES", ""} {
		requirePgError(t, insertUserLanguage(pool, ana, code, "spoken", 7, 0), foreignKeyViolation, "user_languages_language_code_fkey")
	}

	if err := insertUserLanguage(pool, ana, "es", "spoken", 7, 0); err != nil {
		t.Fatal(err)
	}
	if err := insertUserLanguage(pool, ana, "ja", "learning", 2, 0); err != nil {
		t.Fatal(err)
	}
	if err := insertUserLanguage(pool, ben, "ja", "spoken", 7, 0); err != nil {
		t.Fatal(err)
	}

	// A language somebody has can't leave the catalog or change its code.
	_, err := pool.Exec(ctx, `DELETE FROM languages WHERE code = 'es'`)
	requirePgError(t, err, foreignKeyViolation, "user_languages_language_code_fkey")
	_, err = pool.Exec(ctx, `UPDATE languages SET code = 'spa' WHERE code = 'es'`)
	requirePgError(t, err, foreignKeyViolation, "user_languages_language_code_fkey")
	if n := countRows(t, pool, `SELECT count(*) FROM languages WHERE code = 'es'`); n != 1 {
		t.Fatalf("es is in the catalog %d times, want 1", n)
	}

	// Deleting a user deletes their languages and nobody else's.
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, ana); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM user_languages WHERE user_id = $1`, ana); n != 0 {
		t.Errorf("languages of a deleted user = %d, want 0", n)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM user_languages WHERE user_id = $1`, ben); n != 1 {
		t.Errorf("languages of the other user = %d, want 1", n)
	}
}

// The lookup discovery will make (who speaks or learns a language, from a
// level up) has its index, in that column order.
func TestUserLanguagesByLanguageIndex(t *testing.T) {
	pool := testutil.DB(t)
	var def string
	err := pool.QueryRow(context.Background(),
		`SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' AND indexname = 'user_languages_by_language'`).Scan(&def)
	if err != nil {
		t.Fatalf("index user_languages_by_language: %v", err)
	}
	if !strings.Contains(def, "(language_code, kind, level)") {
		t.Errorf("index definition = %q, want (language_code, kind, level)", def)
	}
}

// canonicalUUID is the only spelling of a public identifier: lowercase, with
// its hyphens.
var canonicalUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func publicID(t *testing.T, pool *pgxpool.Pool, userID string) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `SELECT public_id FROM profiles WHERE user_id = $1`, userID).Scan(&id)
	if err != nil {
		t.Fatalf("public id of %s: %v", userID, err)
	}
	return id
}

func columnExists(t *testing.T, pool *pgxpool.Pool, table, column string) bool {
	t.Helper()
	return countRows(t, pool,
		`SELECT count(*) FROM information_schema.columns
		 WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2`, table, column) == 1
}

// Every profile gets a public identifier of its own when it is created,
// without the writer naming one, and it is neither the user's id nor empty.
func TestProfilesPublicID(t *testing.T) {
	ctx := context.Background()
	pool := testutil.DB(t)
	ana := mustInsertUser(t, pool, "ana@example.com")
	ben := mustInsertUser(t, pool, "ben@example.com")
	if err := insertProfile(pool, ana, "Ana", ""); err != nil {
		t.Fatal(err)
	}
	if err := insertProfile(pool, ben, "Ben", ""); err != nil {
		t.Fatal(err)
	}

	anaID, benID := publicID(t, pool, ana), publicID(t, pool, ben)
	for _, id := range []string{anaID, benID} {
		if !canonicalUUID.MatchString(id) {
			t.Errorf("public id %q is not a canonical UUID", id)
		}
	}
	if anaID == benID {
		t.Errorf("two profiles share the public id %s", anaID)
	}
	if anaID == ana || benID == ben || anaID == ben || benID == ana {
		t.Error("a public id is a user's id")
	}

	// Changing the text leaves it alone.
	if _, err := pool.Exec(ctx, `UPDATE profiles SET display_name = 'Ana L.', updated_at = now() WHERE user_id = $1`, ana); err != nil {
		t.Fatal(err)
	}
	if got := publicID(t, pool, ana); got != anaID {
		t.Errorf("public id changed with the name: %s → %s", anaID, got)
	}

	// Two profiles never share one, and none is without.
	_, err := pool.Exec(ctx, `UPDATE profiles SET public_id = $2 WHERE user_id = $1`, ben, anaID)
	requirePgError(t, err, uniqueViolation, "profiles_public_id_key")
	_, err = pool.Exec(ctx, `UPDATE profiles SET public_id = NULL WHERE user_id = $1`, ben)
	requirePgError(t, err, notNullViolation, "")
	if got := publicID(t, pool, ben); got != benID {
		t.Errorf("ben's public id after the refused writes: %s, want %s", got, benID)
	}
}

// 00007 adds profiles.public_id. Up gives every profile already there its
// own identifier and changes nothing else about it, updated_at included;
// down removes the column and keeps the profiles.
func TestMigration00007PublicIDWithExistingRows(t *testing.T) {
	ctx := context.Background()
	pool := testutil.DB(t)
	// Leave the schema migrated whatever happens here.
	t.Cleanup(func() {
		if err := db.Migrate(ctx, pool); err != nil {
			t.Errorf("restoring the schema: %v", err)
		}
	})

	// Before 00007: profiles exist and have no public id.
	if err := db.MigrateDownTo(ctx, pool, 6); err != nil {
		t.Fatalf("down to 00006: %v", err)
	}
	if columnExists(t, pool, "profiles", "public_id") {
		t.Fatal("profiles.public_id exists before 00007")
	}
	users := []string{
		mustInsertUser(t, pool, "ana@example.com"),
		insertGoogleUser(t, pool, "ben@example.com", "1001"),
		mustInsertUser(t, pool, "cho@example.com"),
	}
	for i, uid := range users {
		if err := insertProfile(pool, uid, "Member", "Hi"); err != nil {
			t.Fatal(err)
		}
		// Saved and last changed at different, known times in the past.
		if _, err := pool.Exec(ctx,
			`UPDATE profiles SET created_at = now() - interval '30 days', updated_at = now() - make_interval(days => $2)
			 WHERE user_id = $1`, uid, i+1); err != nil {
			t.Fatal(err)
		}
	}
	type stamps struct{ created, updated time.Time }
	read := func(uid string) (s stamps) {
		t.Helper()
		err := pool.QueryRow(ctx, `SELECT created_at, updated_at FROM profiles WHERE user_id = $1 AND display_name = 'Member' AND bio = 'Hi'`,
			uid).Scan(&s.created, &s.updated)
		if err != nil {
			t.Fatalf("profile of %s: %v", uid, err)
		}
		return s
	}
	before := map[string]stamps{}
	for _, uid := range users {
		before[uid] = read(uid)
	}

	// Up with profiles present: each gets its own id, nothing else moves.
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("up with profiles present: %v", err)
	}
	seen := map[string]bool{}
	for _, uid := range users {
		id := publicID(t, pool, uid)
		if !canonicalUUID.MatchString(id) {
			t.Errorf("public id %q is not a canonical UUID", id)
		}
		if seen[id] {
			t.Errorf("two existing profiles got the public id %s", id)
		}
		seen[id] = true
		if got := read(uid); !got.created.Equal(before[uid].created) || !got.updated.Equal(before[uid].updated) {
			t.Errorf("timestamps moved: %+v → %+v", before[uid], got)
		}
	}
	if n := countRows(t, pool, `SELECT count(*) FROM profiles p JOIN users u ON u.id = p.public_id`); n != 0 {
		t.Errorf("%d public ids are a user's id", n)
	}

	// Down with profiles present: the column goes, the profiles stay.
	if err := db.MigrateDownTo(ctx, pool, 6); err != nil {
		t.Fatalf("down with profiles present: %v", err)
	}
	if columnExists(t, pool, "profiles", "public_id") {
		t.Error("profiles.public_id still exists after rolling 00007 back")
	}
	for _, uid := range users {
		if got := read(uid); !got.updated.Equal(before[uid].updated) {
			t.Errorf("updated_at moved by the rollback: %v → %v", before[uid].updated, got.updated)
		}
	}

	// Up again: every profile has an id once more, and a new profile gets one.
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("up again: %v", err)
	}
	dee := mustInsertUser(t, pool, "dee@example.com")
	if err := insertProfile(pool, dee, "Dee", ""); err != nil {
		t.Errorf("saving a profile after re-applying: %v", err)
	}
	if n := countRows(t, pool, `SELECT count(DISTINCT public_id) FROM profiles`); n != len(users)+1 {
		t.Errorf("distinct public ids after re-applying = %d, want %d", n, len(users)+1)
	}
}

func insertAvatar(pool *pgxpool.Pool, userID string, image, sourceHash []byte) error {
	_, err := pool.Exec(context.Background(),
		`INSERT INTO avatars (user_id, image, source_sha256) VALUES ($1, $2, $3)`, userID, image, sourceHash)
	return err
}

func TestAvatarsDefaultsAndNotNull(t *testing.T) {
	ctx := context.Background()
	pool := testutil.DB(t)
	uid := mustInsertUser(t, pool, "ana@example.com")
	if err := insertAvatar(pool, uid, []byte("jpeg"), hash(1)); err != nil {
		t.Fatal(err)
	}

	var createdAt, updatedAt time.Time
	if err := pool.QueryRow(ctx, `SELECT created_at, updated_at FROM avatars WHERE user_id = $1`, uid).
		Scan(&createdAt, &updatedAt); err != nil {
		t.Fatal(err)
	}
	if createdAt.IsZero() || updatedAt.IsZero() {
		t.Error("timestamps must default to now()")
	}
	for _, column := range []string{"image", "source_sha256", "created_at", "updated_at"} {
		_, err := pool.Exec(ctx, `UPDATE avatars SET `+column+` = NULL WHERE user_id = $1`, uid)
		requirePgError(t, err, notNullViolation, "")
	}
}

// The primary key is the user: a second picture for the same user is refused.
// A picture needs no profile.
func TestAvatarsOnePerUser(t *testing.T) {
	pool := testutil.DB(t)
	ana := mustInsertUser(t, pool, "ana@example.com")
	ben := mustInsertUser(t, pool, "ben@example.com")
	if err := insertAvatar(pool, ana, []byte("one"), hash(1)); err != nil {
		t.Fatal(err)
	}
	requirePgError(t, insertAvatar(pool, ana, []byte("two"), hash(2)), uniqueViolation, "avatars_pkey")

	// Two members may upload the same file: the hash is not a key.
	if err := insertAvatar(pool, ben, []byte("one"), hash(1)); err != nil {
		t.Errorf("another user with the same picture: %v", err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM profiles`); n != 0 {
		t.Errorf("profiles = %d, want 0: a picture needs none", n)
	}
}

func TestAvatarsImageSize(t *testing.T) {
	pool := testutil.DB(t)
	ana := mustInsertUser(t, pool, "ana@example.com")
	ben := mustInsertUser(t, pool, "ben@example.com")
	cho := mustInsertUser(t, pool, "cho@example.com")
	const limit = 512 << 10

	requirePgError(t, insertAvatar(pool, ana, []byte{}, hash(1)), checkViolation, "avatars_image_size")
	requirePgError(t, insertAvatar(pool, ana, bytes.Repeat([]byte{0xFF}, limit+1), hash(1)), checkViolation, "avatars_image_size")
	if err := insertAvatar(pool, ben, []byte{0xFF}, hash(1)); err != nil {
		t.Errorf("one-byte image rejected: %v", err)
	}
	// The limit counts bytes, zero bytes included.
	if err := insertAvatar(pool, cho, make([]byte, limit), hash(1)); err != nil {
		t.Errorf("image of exactly the limit rejected: %v", err)
	}
}

func TestAvatarsSourceHashMustBe32Bytes(t *testing.T) {
	pool := testutil.DB(t)
	uid := mustInsertUser(t, pool, "ana@example.com")
	for _, h := range [][]byte{{}, []byte("too-short"), bytes.Repeat([]byte{1}, 31), bytes.Repeat([]byte{1}, 33)} {
		requirePgError(t, insertAvatar(pool, uid, []byte("jpeg"), h), checkViolation, "avatars_source_sha256_length")
	}
	if err := insertAvatar(pool, uid, []byte("jpeg"), hash(1)); err != nil {
		t.Errorf("32-byte hash rejected: %v", err)
	}
}

func TestAvatarsBelongToAnExistingUser(t *testing.T) {
	ctx := context.Background()
	pool := testutil.DB(t)
	requirePgError(t, insertAvatar(pool, "00000000-0000-0000-0000-000000000000", []byte("jpeg"), hash(1)),
		foreignKeyViolation, "avatars_user_id_fkey")

	ana := mustInsertUser(t, pool, "ana@example.com")
	ben := mustInsertUser(t, pool, "ben@example.com")
	if err := insertAvatar(pool, ana, []byte("ana"), hash(1)); err != nil {
		t.Fatal(err)
	}
	if err := insertAvatar(pool, ben, []byte("ben"), hash(2)); err != nil {
		t.Fatal(err)
	}

	// Deleting a user deletes their picture and nobody else's.
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, ana); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM avatars WHERE user_id = $1`, ana); n != 0 {
		t.Errorf("pictures of a deleted user = %d, want 0", n)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM avatars WHERE user_id = $1 AND image = 'ben'`, ben); n != 1 {
		t.Errorf("pictures of the other user = %d, want 1", n)
	}
}

// testutil.DB empties the table like every table of user data.
func TestAvatarsAreTruncatedBetweenTests(t *testing.T) {
	pool := testutil.DB(t)
	uid := mustInsertUser(t, pool, "ana@example.com")
	if err := insertAvatar(pool, uid, []byte("jpeg"), hash(1)); err != nil {
		t.Fatal(err)
	}
	pool = testutil.DB(t)
	if n := countRows(t, pool, `SELECT count(*) FROM avatars`); n != 0 {
		t.Errorf("avatars after testutil.DB = %d, want 0", n)
	}
}

// 00008 adds avatars. Up leaves users and profiles as they were and gives
// nobody a picture; down removes the table with whatever pictures it holds
// and touches nothing else.
func TestMigration00008AvatarsWithExistingRows(t *testing.T) {
	ctx := context.Background()
	pool := testutil.DB(t)
	// Leave the schema migrated whatever happens here.
	t.Cleanup(func() {
		if err := db.Migrate(ctx, pool); err != nil {
			t.Errorf("restoring the schema: %v", err)
		}
	})

	// Before 00008: users and profiles exist, the table does not.
	if err := db.MigrateDownTo(ctx, pool, 7); err != nil {
		t.Fatalf("down to 00007: %v", err)
	}
	if columnExists(t, pool, "avatars", "user_id") {
		t.Fatal("avatars exists before 00008")
	}
	ana := mustInsertUser(t, pool, "ana@example.com")
	ben := insertGoogleUser(t, pool, "ben@example.com", "1001")
	if err := insertProfile(pool, ana, "Ana", "Hi"); err != nil {
		t.Fatal(err)
	}
	anaID := publicID(t, pool, ana)

	// Up with users and a profile present: nothing is backfilled.
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("up with users present: %v", err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM avatars`); n != 0 {
		t.Errorf("avatars after migrating = %d, want 0: existing users get none", n)
	}
	if err := insertAvatar(pool, ana, []byte("ana"), hash(1)); err != nil {
		t.Fatal(err)
	}
	if err := insertAvatar(pool, ben, []byte("ben"), hash(2)); err != nil {
		t.Fatal(err)
	}

	// Down with pictures present: they go; users, identities and the profile
	// with its public id stay.
	if err := db.MigrateDownTo(ctx, pool, 7); err != nil {
		t.Fatalf("down with pictures present: %v", err)
	}
	if columnExists(t, pool, "avatars", "user_id") {
		t.Error("avatars still exists after rolling 00008 back")
	}
	if n := countRows(t, pool, `SELECT count(*) FROM users u LEFT JOIN user_identities i ON i.user_id = u.id`); n != 2 {
		t.Errorf("users after rolling back = %d, want 2", n)
	}
	if got := publicID(t, pool, ana); got != anaID {
		t.Errorf("ana's public id after rolling back: %s, want %s", got, anaID)
	}

	// Up again: an empty table, and the same users can set a picture.
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("up again: %v", err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM avatars`); n != 0 {
		t.Errorf("avatars after re-applying = %d, want 0", n)
	}
	if err := insertAvatar(pool, ana, []byte("ana"), hash(1)); err != nil {
		t.Errorf("setting a picture after re-applying: %v", err)
	}
}

func insertBlock(pool *pgxpool.Pool, blockerID, blockedID string) error {
	_, err := pool.Exec(context.Background(),
		`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)`, blockerID, blockedID)
	return err
}

func TestBlocksDefaultsAndNotNull(t *testing.T) {
	ctx := context.Background()
	pool := testutil.DB(t)
	ana := mustInsertUser(t, pool, "ana@example.com")
	ben := mustInsertUser(t, pool, "ben@example.com")
	if err := insertBlock(pool, ana, ben); err != nil {
		t.Fatal(err)
	}

	var createdAt time.Time
	if err := pool.QueryRow(ctx, `SELECT created_at FROM blocks WHERE blocker_id = $1`, ana).Scan(&createdAt); err != nil {
		t.Fatal(err)
	}
	if createdAt.IsZero() {
		t.Error("created_at must default to now()")
	}
	for _, column := range []string{"blocker_id", "blocked_id", "created_at"} {
		_, err := pool.Exec(ctx, `UPDATE blocks SET `+column+` = NULL WHERE blocker_id = $1`, ana)
		requirePgError(t, err, notNullViolation, "")
	}
}

// The primary key is the pair in its direction: a member blocks another at
// most once, and the reverse row is the other member's own block.
func TestBlocksOneRowPerDirection(t *testing.T) {
	pool := testutil.DB(t)
	ana := mustInsertUser(t, pool, "ana@example.com")
	ben := mustInsertUser(t, pool, "ben@example.com")
	cho := mustInsertUser(t, pool, "cho@example.com")
	if err := insertBlock(pool, ana, ben); err != nil {
		t.Fatal(err)
	}
	requirePgError(t, insertBlock(pool, ana, ben), uniqueViolation, "blocks_pkey")

	if err := insertBlock(pool, ben, ana); err != nil {
		t.Errorf("the reverse block: %v", err)
	}
	// A member blocks several, and several block one member.
	if err := insertBlock(pool, ana, cho); err != nil {
		t.Errorf("a second block by the same member: %v", err)
	}
	if err := insertBlock(pool, cho, ben); err != nil {
		t.Errorf("a second block of the same member: %v", err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM blocks`); n != 4 {
		t.Errorf("blocks = %d, want 4", n)
	}
}

func TestBlocksNotSelf(t *testing.T) {
	pool := testutil.DB(t)
	ana := mustInsertUser(t, pool, "ana@example.com")
	ben := mustInsertUser(t, pool, "ben@example.com")
	requirePgError(t, insertBlock(pool, ana, ana), checkViolation, "blocks_not_self")

	// Nor by changing a stored block.
	if err := insertBlock(pool, ana, ben); err != nil {
		t.Fatal(err)
	}
	_, err := pool.Exec(context.Background(), `UPDATE blocks SET blocked_id = blocker_id WHERE blocker_id = $1`, ana)
	requirePgError(t, err, checkViolation, "blocks_not_self")
	if n := countRows(t, pool, `SELECT count(*) FROM blocks WHERE blocker_id = blocked_id`); n != 0 {
		t.Errorf("blocks of oneself = %d, want 0", n)
	}
}

// Both members must exist, and a block goes with either account: with the
// blocker's and with the blocked member's.
func TestBlocksBelongToExistingUsers(t *testing.T) {
	ctx := context.Background()
	pool := testutil.DB(t)
	const nobody = "00000000-0000-0000-0000-000000000000"
	ana := mustInsertUser(t, pool, "ana@example.com")
	ben := mustInsertUser(t, pool, "ben@example.com")
	cho := mustInsertUser(t, pool, "cho@example.com")
	dan := mustInsertUser(t, pool, "dan@example.com")
	requirePgError(t, insertBlock(pool, nobody, ana), foreignKeyViolation, "blocks_blocker_id_fkey")
	requirePgError(t, insertBlock(pool, ana, nobody), foreignKeyViolation, "blocks_blocked_id_fkey")

	for _, pair := range [][2]string{{ana, ben}, {ana, cho}, {ben, ana}, {cho, ben}, {cho, dan}} {
		if err := insertBlock(pool, pair[0], pair[1]); err != nil {
			t.Fatal(err)
		}
	}

	// The blocker is deleted: their blocks go, and so does the block of them.
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, ana); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM blocks WHERE $1 IN (blocker_id, blocked_id)`, ana); n != 0 {
		t.Errorf("blocks involving a deleted blocker = %d, want 0", n)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM blocks`); n != 2 {
		t.Errorf("blocks left = %d, want cho's 2", n)
	}

	// The blocked user is deleted: the block of them goes, the blocker's
	// other block stays.
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, ben); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM blocks WHERE blocked_id = $1`, ben); n != 0 {
		t.Errorf("blocks of a deleted user = %d, want 0", n)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM blocks WHERE blocker_id = $1 AND blocked_id = $2`, cho, dan); n != 1 {
		t.Errorf("the blocker's other block = %d rows, want 1", n)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM users`); n != 2 {
		t.Errorf("users = %d, want 2: deleting a block's member deletes nobody else", n)
	}
}

// Finding who blocked a member (the cascade when that member is deleted) has
// its index; the primary key starts with the blocker.
func TestBlocksByBlockedIndex(t *testing.T) {
	pool := testutil.DB(t)
	var def string
	err := pool.QueryRow(context.Background(),
		`SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' AND indexname = 'blocks_blocked_id_idx'`).Scan(&def)
	if err != nil {
		t.Fatalf("index blocks_blocked_id_idx: %v", err)
	}
	if !strings.Contains(def, "(blocked_id)") {
		t.Errorf("index definition = %q, want (blocked_id)", def)
	}
}

// testutil.DB empties the table like every table of user data.
func TestBlocksAreTruncatedBetweenTests(t *testing.T) {
	pool := testutil.DB(t)
	ana := mustInsertUser(t, pool, "ana@example.com")
	ben := mustInsertUser(t, pool, "ben@example.com")
	if err := insertBlock(pool, ana, ben); err != nil {
		t.Fatal(err)
	}
	pool = testutil.DB(t)
	if n := countRows(t, pool, `SELECT count(*) FROM blocks`); n != 0 {
		t.Errorf("blocks after testutil.DB = %d, want 0", n)
	}
}

// 00009 adds blocks. Up leaves every other table as it was and blocks
// nobody; down removes the table with whatever blocks it holds and touches
// nothing else.
func TestMigration00009BlocksWithExistingRows(t *testing.T) {
	ctx := context.Background()
	pool := testutil.DB(t)
	// Leave the schema migrated whatever happens here.
	t.Cleanup(func() {
		if err := db.Migrate(ctx, pool); err != nil {
			t.Errorf("restoring the schema: %v", err)
		}
	})

	// Before 00009: users, a profile and a picture exist, the table does not.
	if err := db.MigrateDownTo(ctx, pool, 8); err != nil {
		t.Fatalf("down to 00008: %v", err)
	}
	if columnExists(t, pool, "blocks", "blocker_id") {
		t.Fatal("blocks exists before 00009")
	}
	ana := mustInsertUser(t, pool, "ana@example.com")
	ben := insertGoogleUser(t, pool, "ben@example.com", "1001")
	if err := insertProfile(pool, ana, "Ana", "Hi"); err != nil {
		t.Fatal(err)
	}
	if err := insertAvatar(pool, ana, []byte("ana"), hash(1)); err != nil {
		t.Fatal(err)
	}
	anaID := publicID(t, pool, ana)

	// Up with users present: nobody is blocked.
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("up with users present: %v", err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM blocks`); n != 0 {
		t.Errorf("blocks after migrating = %d, want 0", n)
	}
	if err := insertBlock(pool, ana, ben); err != nil {
		t.Fatal(err)
	}
	if err := insertBlock(pool, ben, ana); err != nil {
		t.Fatal(err)
	}

	// Down with blocks present: they go; users, identities, the profile with
	// its public id and the picture stay.
	if err := db.MigrateDownTo(ctx, pool, 8); err != nil {
		t.Fatalf("down with blocks present: %v", err)
	}
	if columnExists(t, pool, "blocks", "blocker_id") {
		t.Error("blocks still exists after rolling 00009 back")
	}
	if n := countRows(t, pool, `SELECT count(*) FROM users u LEFT JOIN user_identities i ON i.user_id = u.id`); n != 2 {
		t.Errorf("users after rolling back = %d, want 2", n)
	}
	if got := publicID(t, pool, ana); got != anaID {
		t.Errorf("ana's public id after rolling back: %s, want %s", got, anaID)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM avatars WHERE user_id = $1`, ana); n != 1 {
		t.Errorf("ana's pictures after rolling back = %d, want 1", n)
	}

	// Up again: an empty table, and the same users can block.
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("up again: %v", err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM blocks`); n != 0 {
		t.Errorf("blocks after re-applying = %d, want 0", n)
	}
	if err := insertBlock(pool, ana, ben); err != nil {
		t.Errorf("blocking after re-applying: %v", err)
	}
}

func insertReport(pool *pgxpool.Pool, reporterID, reportedID, reason, details string) error {
	_, err := pool.Exec(context.Background(),
		`INSERT INTO reports (reporter_id, reported_id, reason, details) VALUES ($1, $2, $3, $4)`,
		reporterID, reportedID, reason, details)
	return err
}

// A report holds who, about whom, why and when, and nothing else: no column
// can hold a copy of the reported member's name, text, languages or picture.
func TestReportsColumnsAreExactlyTheReport(t *testing.T) {
	pool := testutil.DB(t)
	rows, err := pool.Query(context.Background(),
		`SELECT column_name::text FROM information_schema.columns
		 WHERE table_schema = 'public' AND table_name = 'reports' ORDER BY ordinal_position`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	want := "id, reporter_id, reported_id, reason, details, created_at, updated_at"
	if strings.Join(got, ", ") != want {
		t.Errorf("columns of reports = %s, want exactly %s", strings.Join(got, ", "), want)
	}
}

func TestReportsDefaultsAndNotNull(t *testing.T) {
	ctx := context.Background()
	pool := testutil.DB(t)
	ana := mustInsertUser(t, pool, "ana@example.com")
	ben := mustInsertUser(t, pool, "ben@example.com")
	if _, err := pool.Exec(ctx,
		`INSERT INTO reports (reporter_id, reported_id, reason) VALUES ($1, $2, 'spam')`, ana, ben); err != nil {
		t.Fatal(err)
	}

	var (
		id, details          string
		createdAt, updatedAt time.Time
	)
	err := pool.QueryRow(ctx, `SELECT id::text, details, created_at, updated_at FROM reports WHERE reported_id = $1`, ben).
		Scan(&id, &details, &createdAt, &updatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if id == "" || details != "" || createdAt.IsZero() || !updatedAt.Equal(createdAt) {
		t.Errorf("defaults: id %q, details %q, created_at %v, updated_at %v; want an id, no details and one time",
			id, details, createdAt, updatedAt)
	}
	// Everything but the reporter is required.
	for _, column := range []string{"id", "reported_id", "reason", "details", "created_at", "updated_at"} {
		_, err := pool.Exec(ctx, `UPDATE reports SET `+column+` = NULL WHERE reported_id = $1`, ben)
		requirePgError(t, err, notNullViolation, "")
	}
}

// One report by one member about another; the reverse pair, another reporter
// and another reported member are each a report of their own.
func TestReportsOnePerReporterAndReported(t *testing.T) {
	pool := testutil.DB(t)
	ana := mustInsertUser(t, pool, "ana@example.com")
	ben := mustInsertUser(t, pool, "ben@example.com")
	cho := mustInsertUser(t, pool, "cho@example.com")
	if err := insertReport(pool, ana, ben, "spam", ""); err != nil {
		t.Fatal(err)
	}
	requirePgError(t, insertReport(pool, ana, ben, "other", "again"), uniqueViolation, "reports_reporter_reported_key")

	if err := insertReport(pool, ben, ana, "spam", ""); err != nil {
		t.Errorf("the reverse report: %v", err)
	}
	if err := insertReport(pool, cho, ben, "spam", ""); err != nil {
		t.Errorf("a second reporter of the same member: %v", err)
	}
	if err := insertReport(pool, ana, cho, "spam", ""); err != nil {
		t.Errorf("a second report by the same member: %v", err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM reports`); n != 4 {
		t.Errorf("reports = %d, want 4", n)
	}
}

func TestReportsNotSelf(t *testing.T) {
	pool := testutil.DB(t)
	ana := mustInsertUser(t, pool, "ana@example.com")
	ben := mustInsertUser(t, pool, "ben@example.com")
	requirePgError(t, insertReport(pool, ana, ana, "spam", ""), checkViolation, "reports_not_self")

	// Nor by changing a stored report.
	if err := insertReport(pool, ana, ben, "spam", ""); err != nil {
		t.Fatal(err)
	}
	_, err := pool.Exec(context.Background(), `UPDATE reports SET reported_id = reporter_id WHERE reporter_id = $1`, ana)
	requirePgError(t, err, checkViolation, "reports_not_self")
	if n := countRows(t, pool, `SELECT count(*) FROM reports WHERE reporter_id = reported_id`); n != 0 {
		t.Errorf("reports of oneself = %d, want 0", n)
	}
}

func TestReportsReason(t *testing.T) {
	pool := testutil.DB(t)
	ana := mustInsertUser(t, pool, "ana@example.com")
	for i, reason := range []string{"harassment", "inappropriate_content", "spam", "impersonation", "other"} {
		reported := mustInsertUser(t, pool, "reported"+string(rune('a'+i))+"@example.com")
		if err := insertReport(pool, ana, reported, reason, ""); err != nil {
			t.Errorf("reason %q: %v", reason, err)
		}
	}
	ben := mustInsertUser(t, pool, "ben@example.com")
	for _, reason := range []string{"", "Spam", " spam", "spam ", "rude", "harassment,spam"} {
		requirePgError(t, insertReport(pool, ana, ben, reason, ""), checkViolation, "reports_reason")
	}
}

// The limit is in characters, not bytes, like the profile's text.
func TestReportsDetailsLength(t *testing.T) {
	pool := testutil.DB(t)
	ana := mustInsertUser(t, pool, "ana@example.com")
	ben := mustInsertUser(t, pool, "ben@example.com")
	cho := mustInsertUser(t, pool, "cho@example.com")
	if err := insertReport(pool, ana, ben, "other", strings.Repeat("語", 1000)); err != nil {
		t.Errorf("1000 characters of three bytes each: %v", err)
	}
	requirePgError(t, insertReport(pool, ana, cho, "other", strings.Repeat("a", 1001)),
		checkViolation, "reports_details_length")
}

// A report goes with the account it is about, and stays when its author's
// account goes: about the same member, with the same content and no reporter.
func TestReportsFollowTheReportedAndOutliveTheReporter(t *testing.T) {
	ctx := context.Background()
	pool := testutil.DB(t)
	const nobody = "00000000-0000-0000-0000-000000000000"
	ana := mustInsertUser(t, pool, "ana@example.com")
	ben := mustInsertUser(t, pool, "ben@example.com")
	cho := mustInsertUser(t, pool, "cho@example.com")
	dan := mustInsertUser(t, pool, "dan@example.com")
	requirePgError(t, insertReport(pool, nobody, ana, "spam", ""), foreignKeyViolation, "reports_reporter_id_fkey")
	requirePgError(t, insertReport(pool, ana, nobody, "spam", ""), foreignKeyViolation, "reports_reported_id_fkey")

	for _, r := range []struct{ reporter, reported, reason, details string }{
		{ana, ben, "spam", "from ana"},
		{cho, ben, "harassment", "from cho"},
		{ana, dan, "other", "about dan"},
		{ben, dan, "spam", ""},
	} {
		if err := insertReport(pool, r.reporter, r.reported, r.reason, r.details); err != nil {
			t.Fatal(err)
		}
	}
	var before time.Time
	if err := pool.QueryRow(ctx, `SELECT updated_at FROM reports WHERE reporter_id = $1 AND reported_id = $2`, ana, ben).
		Scan(&before); err != nil {
		t.Fatal(err)
	}

	// The reporters are deleted: their reports stay, and name nobody.
	for _, reporter := range []string{ana, cho} {
		if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, reporter); err != nil {
			t.Fatal(err)
		}
	}
	if n := countRows(t, pool, `SELECT count(*) FROM reports`); n != 4 {
		t.Errorf("reports after deleting two reporters = %d, want 4", n)
	}
	// Two rows about one member with no reporter coexist.
	rows, err := pool.Query(ctx,
		`SELECT reason || ':' || details || ':' || (updated_at = created_at)::text FROM reports
		 WHERE reported_id = $1 AND reporter_id IS NULL ORDER BY details`, ben)
	if err != nil {
		t.Fatal(err)
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if want := "spam:from ana:true, harassment:from cho:true"; strings.Join(got, ", ") != want {
		t.Errorf("reports about ben with no reporter = %v, want %s", got, want)
	}
	var after time.Time
	if err := pool.QueryRow(ctx, `SELECT updated_at FROM reports WHERE details = 'from ana'`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !after.Equal(before) {
		t.Errorf("updated_at moved when the reporter was deleted: %v, was %v", after, before)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM reports WHERE reported_id = $1 AND reporter_id IS NULL`, dan); n != 1 {
		t.Errorf("ana's report about dan = %d rows with no reporter, want 1", n)
	}

	// The reported user is deleted: every report about them goes, with and
	// without a reporter. Their own report of dan stays, without them.
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, ben); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM reports WHERE reported_id = $1`, ben); n != 0 {
		t.Errorf("reports about a deleted user = %d, want 0", n)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM reports WHERE reported_id = $1 AND reporter_id IS NULL`, dan); n != 2 {
		t.Errorf("reports about dan = %d, want 2 with no reporter", n)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM users`); n != 1 {
		t.Errorf("users = %d, want dan: deleting a report's member deletes nobody else", n)
	}
}

// Reports are read per reported member, and the cascade finds them the same
// way; the unique constraint starts with the reporter.
func TestReportsByReportedIndex(t *testing.T) {
	pool := testutil.DB(t)
	var def string
	err := pool.QueryRow(context.Background(),
		`SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' AND indexname = 'reports_reported_id_idx'`).Scan(&def)
	if err != nil {
		t.Fatalf("index reports_reported_id_idx: %v", err)
	}
	if !strings.Contains(def, "(reported_id)") {
		t.Errorf("index definition = %q, want (reported_id)", def)
	}
}

// testutil.DB empties the table like every table of user data.
func TestReportsAreTruncatedBetweenTests(t *testing.T) {
	pool := testutil.DB(t)
	ana := mustInsertUser(t, pool, "ana@example.com")
	ben := mustInsertUser(t, pool, "ben@example.com")
	if err := insertReport(pool, ana, ben, "spam", ""); err != nil {
		t.Fatal(err)
	}
	pool = testutil.DB(t)
	if n := countRows(t, pool, `SELECT count(*) FROM reports`); n != 0 {
		t.Errorf("reports after testutil.DB = %d, want 0", n)
	}
}

// 00010 adds reports. Up leaves every other table as it was and reports
// nobody; down removes the table with whatever reports it holds and touches
// nothing else, blocks included.
func TestMigration00010ReportsWithExistingRows(t *testing.T) {
	ctx := context.Background()
	pool := testutil.DB(t)
	// Leave the schema migrated whatever happens here.
	t.Cleanup(func() {
		if err := db.Migrate(ctx, pool); err != nil {
			t.Errorf("restoring the schema: %v", err)
		}
	})

	// Before 00010: users, a profile, a picture and a block exist, the table
	// does not.
	if err := db.MigrateDownTo(ctx, pool, 9); err != nil {
		t.Fatalf("down to 00009: %v", err)
	}
	if columnExists(t, pool, "reports", "reporter_id") {
		t.Fatal("reports exists before 00010")
	}
	ana := mustInsertUser(t, pool, "ana@example.com")
	ben := insertGoogleUser(t, pool, "ben@example.com", "1001")
	if err := insertProfile(pool, ana, "Ana", "Hi"); err != nil {
		t.Fatal(err)
	}
	if err := insertAvatar(pool, ana, []byte("ana"), hash(1)); err != nil {
		t.Fatal(err)
	}
	if err := insertBlock(pool, ana, ben); err != nil {
		t.Fatal(err)
	}
	anaID := publicID(t, pool, ana)

	// Up with users present: nobody is reported.
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("up with users present: %v", err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM reports`); n != 0 {
		t.Errorf("reports after migrating = %d, want 0", n)
	}
	if err := insertReport(pool, ana, ben, "spam", "details"); err != nil {
		t.Fatal(err)
	}
	if err := insertReport(pool, ben, ana, "other", ""); err != nil {
		t.Fatal(err)
	}

	// Down with reports present: they go; users, identities, the profile with
	// its public id, the picture and the block stay.
	if err := db.MigrateDownTo(ctx, pool, 9); err != nil {
		t.Fatalf("down with reports present: %v", err)
	}
	if columnExists(t, pool, "reports", "reporter_id") {
		t.Error("reports still exists after rolling 00010 back")
	}
	if n := countRows(t, pool, `SELECT count(*) FROM users u LEFT JOIN user_identities i ON i.user_id = u.id`); n != 2 {
		t.Errorf("users after rolling back = %d, want 2", n)
	}
	if got := publicID(t, pool, ana); got != anaID {
		t.Errorf("ana's public id after rolling back: %s, want %s", got, anaID)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM avatars WHERE user_id = $1`, ana); n != 1 {
		t.Errorf("ana's pictures after rolling back = %d, want 1", n)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM blocks WHERE blocker_id = $1 AND blocked_id = $2`, ana, ben); n != 1 {
		t.Errorf("ana's block after rolling back = %d rows, want 1", n)
	}

	// Up again: an empty table, and the same users can report.
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("up again: %v", err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM reports`); n != 0 {
		t.Errorf("reports after re-applying = %d, want 0", n)
	}
	if err := insertReport(pool, ana, ben, "spam", ""); err != nil {
		t.Errorf("reporting after re-applying: %v", err)
	}
}
