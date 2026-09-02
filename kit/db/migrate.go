package db

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// lockKey serializes migrations across processes and replicas: whoever holds
// the advisory lock runs the ledger, everyone else waits and then finds nothing
// to do.
const lockKey = 7240101

// Migrate applies every pending migration in fsys, in filename order, against
// the single "schema_migrations" ledger. migrateURL connects as the owner role,
// which holds the DDL rights the application role deliberately lacks.
func Migrate(ctx context.Context, migrateURL string, fsys fs.FS) error {
	c, err := openOwner(migrateURL)
	if err != nil {
		return err
	}
	defer c.Close()

	sqlDB, err := c.db.DB()
	if err != nil {
		return fmt.Errorf("db: migrate: %w", err)
	}
	// One pinned connection: the advisory lock is session-scoped, so locking
	// and unlocking have to happen on the same connection.
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("db: migrate: connect: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", lockKey); err != nil {
		return fmt.Errorf("db: migrate: lock: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", lockKey)
	}()

	driver, err := migratepg.WithConnection(ctx, conn, &migratepg.Config{MigrationsTable: "schema_migrations"})
	if err != nil {
		return fmt.Errorf("db: migrate: driver: %w", err)
	}
	source, err := iofs.New(fsys, ".")
	if err != nil {
		return fmt.Errorf("db: migrate: source: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	if err != nil {
		return fmt.Errorf("db: migrate: instance: %w", err)
	}
	// Nothing to apply is the ordinary outcome on every boot after the first.
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("db: migrate: up: %w", err)
	}
	return nil
}
