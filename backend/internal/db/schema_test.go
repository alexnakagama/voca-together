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
