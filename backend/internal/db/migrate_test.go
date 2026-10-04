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
	tables := []string{"users", "user_tokens", "sessions", "user_identities"}

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
