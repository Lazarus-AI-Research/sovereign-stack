// Package database owns the sovereign_control Postgres connection and
// embedded migrations.
package database

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrate applies embedded migrations to a postgres:// URL.
func Migrate(databaseURL string) error {
	source, err := iofs.New(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	// golang-migrate's pgx/v5 driver registers the pgx5:// scheme.
	migrateURL := databaseURL
	for _, prefix := range []string{"postgresql://", "postgres://"} {
		if rest, ok := strings.CutPrefix(databaseURL, prefix); ok {
			migrateURL = "pgx5://" + rest
			break
		}
	}
	migrator, err := migrate.NewWithSourceInstance("iofs", source, migrateURL)
	if err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	defer migrator.Close()
	if err := migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrations: %w", err)
	}
	return nil
}

// Pool aliases pgxpool.Pool so callers outside internal need not import pgx.
type Pool = pgxpool.Pool

func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: %w", err)
	}
	return pool, nil
}
