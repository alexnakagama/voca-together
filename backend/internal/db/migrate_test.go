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
	tables := []string{"users", "user_tokens", "sessions"}

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
