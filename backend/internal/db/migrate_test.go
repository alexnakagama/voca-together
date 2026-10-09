package db_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"vocatogether/backend/internal/db"
	"vocatogether/backend/internal/testutil"
)

func tableExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var ok bool
	err := pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)`,
		name).Scan(&ok)
	if err != nil {
		t.Fatalf("checking table %s: %v", name, err)
	}
	return ok
}

// The schema must build from an empty database and be fully reversible.
func TestMigrationsApplyFromEmptyAndRollBack(t *testing.T) {
	ctx := context.Background()
	pool := testutil.DB(t)
	tables := []string{"users", "user_tokens", "sessions", "user_identities", "profiles", "languages", "user_languages", "avatars", "blocks"}

	if err := db.MigrateDownAll(ctx, pool); err != nil {
		t.Fatalf("down: %v", err)
	}
	for _, tbl := range tables {
		if tableExists(t, pool, tbl) {
			t.Errorf("table %s still exists after rolling back", tbl)
		}
	}

	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("up: %v", err)
	}
	for _, tbl := range tables {
		if !tableExists(t, pool, tbl) {
			t.Errorf("table %s missing after migrating", tbl)
		}
	}

	// Running again is a no-op, not an error (it runs on every startup).
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("second up: %v", err)
	}
}

// 00004 drops the table that remembered accepted Google ID tokens (decision
// 026). Up removes it with whatever rows it holds; down brings back the
// empty table exactly as 00003 defined it, so 00003's own down still works.
func TestMigration00004DropsGoogleIDTokenUses(t *testing.T) {
	ctx := context.Background()
	pool := testutil.DB(t)
	const table = "google_id_token_uses"
	// Leave the schema migrated whatever happens here.
	t.Cleanup(func() {
		if err := db.Migrate(ctx, pool); err != nil {
			t.Errorf("restoring the schema: %v", err)
		}
	})

	if tableExists(t, pool, table) {
		t.Fatalf("%s exists in the current schema", table)
	}

	// Down: the table is back, with 00003's constraints.
	if err := db.MigrateDownTo(ctx, pool, 3); err != nil {
		t.Fatalf("down to 00003: %v", err)
	}
	if !tableExists(t, pool, table) {
		t.Fatalf("%s missing after rolling 00004 back", table)
	}
	insert := func(h []byte) error {
		_, err := pool.Exec(ctx,
			`INSERT INTO google_id_token_uses (token_hash, expires_at) VALUES ($1, now() + interval '1 hour')`, h)
		return err
	}
	if err := insert(hash(1)); err != nil {
		t.Fatal(err)
	}
	requirePgError(t, insert(hash(1)), uniqueViolation, "google_id_token_uses_pkey")
	requirePgError(t, insert([]byte("short")), checkViolation, "google_id_token_uses_token_hash_length")

	// Up: gone again, although it held a row (as a deployed database will).
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("up with a row present: %v", err)
	}
	if tableExists(t, pool, table) {
		t.Errorf("%s still exists after 00004", table)
	}

	// And the whole schema still rolls back and rebuilds through 00004.
	if err := db.MigrateDownAll(ctx, pool); err != nil {
		t.Fatalf("down all: %v", err)
	}
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("up from empty: %v", err)
	}
	if tableExists(t, pool, table) {
		t.Errorf("%s exists after migrating from empty", table)
	}
}

// Rolling back 00003 would have to invent passwords or delete accounts; it
// refuses instead and leaves the schema as it was.
func TestMigrationDownRefusesToDropPasswordlessUsers(t *testing.T) {
	ctx := context.Background()
	pool := testutil.DB(t)
	id := insertGoogleUser(t, pool, "ana@example.com", "1001")

	if err := db.MigrateDownAll(ctx, pool); err == nil {
		t.Fatal("rolled back with a passwordless user present")
	}
	if n := countRows(t, pool, `SELECT count(*) FROM users u JOIN user_identities i ON i.user_id = u.id WHERE u.id = $1`, id); n != 1 {
		t.Errorf("passwordless user or identity lost by the failed rollback (%d rows)", n)
	}
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("up after the failed rollback: %v", err)
	}
}

// 00005 adds profiles. Up leaves users as they were and gives nobody a
// profile; down removes the table with whatever profiles it holds and
// touches nothing else, so users survive a rollback and a re-apply.
func TestMigration00005ProfilesWithExistingRows(t *testing.T) {
	ctx := context.Background()
	pool := testutil.DB(t)
	// Leave the schema migrated whatever happens here.
	t.Cleanup(func() {
		if err := db.Migrate(ctx, pool); err != nil {
			t.Errorf("restoring the schema: %v", err)
		}
	})

	// Before 00005: users exist, there is no profiles table.
	if err := db.MigrateDownTo(ctx, pool, 4); err != nil {
		t.Fatalf("down to 00004: %v", err)
	}
	if tableExists(t, pool, "profiles") {
		t.Fatal("profiles exists before 00005")
	}
	ana := mustInsertUser(t, pool, "ana@example.com")
	ben := insertGoogleUser(t, pool, "ben@example.com", "1001")

	// Up with users present: nothing is backfilled.
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("up with users present: %v", err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM profiles`); n != 0 {
		t.Errorf("profiles after migrating = %d, want 0: existing users get none", n)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM users`); n != 2 {
		t.Errorf("users after migrating = %d, want 2", n)
	}
	if err := insertProfile(pool, ana, "Ana", "Hi"); err != nil {
		t.Fatal(err)
	}
	if err := insertProfile(pool, ben, "Ben", ""); err != nil {
		t.Fatal(err)
	}

	// Down with profiles present: they go, the users and their identities stay.
	if err := db.MigrateDownTo(ctx, pool, 4); err != nil {
		t.Fatalf("down with profiles present: %v", err)
	}
	if tableExists(t, pool, "profiles") {
		t.Error("profiles still exists after rolling 00005 back")
	}
	if n := countRows(t, pool, `SELECT count(*) FROM users u LEFT JOIN user_identities i ON i.user_id = u.id`); n != 2 {
		t.Errorf("users after rolling back = %d, want 2", n)
	}

	// Up again: an empty table, and the same users can save a profile.
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("up again: %v", err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM profiles`); n != 0 {
		t.Errorf("profiles after re-applying = %d, want 0", n)
	}
	if err := insertProfile(pool, ana, "Ana", ""); err != nil {
		t.Errorf("saving a profile after re-applying: %v", err)
	}
}

// 00006 adds the language catalog and members' languages. Up leaves users
// and profiles as they were, seeds the catalog and gives nobody a language;
// down removes both tables with whatever they hold and touches nothing else.
func TestMigration00006LanguagesWithExistingRows(t *testing.T) {
	ctx := context.Background()
	pool := testutil.DB(t)
	// Leave the schema migrated whatever happens here.
	t.Cleanup(func() {
		if err := db.Migrate(ctx, pool); err != nil {
			t.Errorf("restoring the schema: %v", err)
		}
	})
	seeded := countRows(t, pool, `SELECT count(*) FROM languages`)

	// Before 00006: users and profiles exist, neither table does.
	if err := db.MigrateDownTo(ctx, pool, 5); err != nil {
		t.Fatalf("down to 00005: %v", err)
	}
	for _, tbl := range []string{"languages", "user_languages"} {
		if tableExists(t, pool, tbl) {
			t.Fatalf("%s exists before 00006", tbl)
		}
	}
	ana := mustInsertUser(t, pool, "ana@example.com")
	ben := insertGoogleUser(t, pool, "ben@example.com", "1001")
	if err := insertProfile(pool, ana, "Ana", "Hi"); err != nil {
		t.Fatal(err)
	}

	// Up with users and a profile present: nothing is backfilled.
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("up with users present: %v", err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM languages`); n != seeded || n == 0 {
		t.Errorf("catalog after migrating = %d languages, want %d", n, seeded)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM user_languages`); n != 0 {
		t.Errorf("user languages after migrating = %d, want 0: existing users get none", n)
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

	// Down with languages in use: both tables go, in an order the foreign
	// key allows, and the users, identities and profiles stay.
	if err := db.MigrateDownTo(ctx, pool, 5); err != nil {
		t.Fatalf("down with languages present: %v", err)
	}
	for _, tbl := range []string{"languages", "user_languages"} {
		if tableExists(t, pool, tbl) {
			t.Errorf("%s still exists after rolling 00006 back", tbl)
		}
	}
	if n := countRows(t, pool, `SELECT count(*) FROM users u LEFT JOIN user_identities i ON i.user_id = u.id`); n != 2 {
		t.Errorf("users after rolling back = %d, want 2", n)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM profiles WHERE display_name = 'Ana' AND bio = 'Hi'`); n != 1 {
		t.Errorf("ana's profile after rolling back: %d rows, want 1", n)
	}

	// Up again: the same catalog, nobody has a language, and the same users
	// can choose some.
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("up again: %v", err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM languages`); n != seeded {
		t.Errorf("catalog after re-applying = %d languages, want %d", n, seeded)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM user_languages`); n != 0 {
		t.Errorf("user languages after re-applying = %d, want 0", n)
	}
	if err := insertUserLanguage(pool, ana, "es", "spoken", 7, 0); err != nil {
		t.Errorf("choosing a language after re-applying: %v", err)
	}
}
