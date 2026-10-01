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
	tables := []string{"users", "user_tokens", "sessions", "user_identities", "google_id_token_uses"}

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
