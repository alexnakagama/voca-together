// Package testutil provides helpers shared by tests that need PostgreSQL.
package testutil

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"vocatogether/backend/internal/db"
)

// DB returns a pool connected to TEST_DATABASE_URL with the schema migrated
// and all tables empty. It skips the test if the variable is unset.
//
// Tests using it share one database, so they must not run in parallel
// (neither t.Parallel nor concurrent packages: use `go test -p 1`).
func DB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; run `make db-up` and `make test`")
	}

	ctx := context.Background()
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect test db: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE users, user_tokens, sessions, user_identities, google_id_token_uses CASCADE`); err != nil {
		t.Fatalf("truncate test db: %v", err)
	}
	return pool
}
