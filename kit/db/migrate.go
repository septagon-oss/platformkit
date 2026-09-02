package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"

	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver "pgx", for the DDL connection
)

// lockKey serializes migrations across processes and replicas: whoever holds
// the advisory lock runs the ledger, everyone else waits and then finds nothing
// to do.
//
// It stacks on golang-migrate's own lock rather than replacing it, on purpose.
// golang-migrate takes an advisory lock derived from the search_path's schema,
// so two deployments migrating different schemas of one database would not see
// each other; this one is a fixed key and therefore per-database, which is what
// "one ledger for the application" means when the schema is a deployment
// choice. Two locks, taken in one order, always by this function.
const lockKey = 7240101

// Migrate applies every pending migration in fsys, in filename order, against
// the single "schema_migrations" ledger. migrateURL connects as the owner role,
// which holds the DDL rights the application role deliberately lacks; the
// ledger and every object land in that connection's search_path.
func Migrate(ctx context.Context, migrateURL string, fsys fs.FS) error {
	pool, err := sql.Open("pgx", migrateURL)
	if err != nil {
		return fmt.Errorf("db: migrate: open: %w", err)
	}
	defer pool.Close()

	// One pinned connection: the advisory lock is session-scoped, so locking
	// and unlocking have to happen on the same connection.
	conn, err := pool.Conn(ctx)
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
