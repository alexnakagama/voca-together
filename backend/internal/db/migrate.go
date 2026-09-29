package db

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

func newProvider(pool *pgxpool.Pool) (*goose.Provider, func() error, error) {
	sqlDB := stdlib.OpenDBFromPool(pool)
	dir, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return nil, nil, err
	}
	p, err := goose.NewProvider(goose.DialectPostgres, sqlDB, dir)
	if err != nil {
		sqlDB.Close()
		return nil, nil, fmt.Errorf("db: migrations: %w", err)
	}
	return p, sqlDB.Close, nil
}

// Migrate applies all pending migrations. It is safe to run on every startup.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	p, closeDB, err := newProvider(pool)
	if err != nil {
		return err
	}
	defer closeDB()
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("db: migrate up: %w", err)
	}
	return nil
}

// MigrateDownAll rolls back every migration. Intended for tests only.
func MigrateDownAll(ctx context.Context, pool *pgxpool.Pool) error {
	p, closeDB, err := newProvider(pool)
	if err != nil {
		return err
	}
	defer closeDB()
	if _, err := p.DownTo(ctx, 0); err != nil {
		return fmt.Errorf("db: migrate down: %w", err)
	}
	return nil
}
