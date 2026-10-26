package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// MigrateOptions is what a migration or a drain run takes beyond its files: the
// budgets its own patience allows and what the composition declares about itself.
// The zero value is what Migrate and Backfill run on — the documented budgets and
// no declaration. A negative duration among Budget's two timeouts refuses the run
// before it connects.
type MigrateOptions struct {
	// Budget is how long one file, and one batch of a drain, may wait. A nil
	// LockTimeout or StatementTimeout is the documented default.
	Budget MigrationBudget
	// Declaration is what this composition says about itself to the files it
	// runs, as session state on the run's own connection. The zero value
	// declares nothing.
	Declaration Declaration
}

// MigrateWithOptions validates the selected histories and applies pending files
// in source and version order, in each file header's transactional, autocommit or
// backfill mode, under opts' budgets and declaration. opts' zero value is what
// Migrate runs on: the documented budgets and no declaration. Invalid files and an
// invalid budget fail before connecting; owner planning fails before that owner's
// pending files run; a worker-owned drain defers later files and returns nil;
// earlier committed files and batches survive later errors. It holds the run's
// advisory lock for its own session and never retries inside it — a run that could
// not take a lock within its budget answers ErrContended and may be run again. See
// README.md for execution modes and backfill ownership.
func MigrateWithOptions(ctx context.Context, migrateURL string, opts MigrateOptions, sources ...MigrationSource) error {
	if err := opts.Budget.validate(); err != nil {
		return fmt.Errorf("db: migrate: %w", err)
	}
	migrations, adoptions, err := readMigrations(sources)
	if err != nil {
		return err
	}
	pool, err := sql.Open("pgx", migrateURL)
	if err != nil {
		return fmt.Errorf("db: migrate: open: %w", err)
	}
	defer pool.Close()
	conn, err := pool.Conn(ctx)
	if err != nil {
		return fmt.Errorf("db: migrate: connect: %w", err)
	}
	defer conn.Close()

	if err := opts.Declaration.declare(ctx, conn); err != nil {
		return fmt.Errorf("db: migrate: %w", err)
	}

	run := &runner{conn: conn, budget: opts.Budget}
	// The composition lock is a session lock and this function owns the session: the two
	// defers above hand it back and close the pool it came from, which ends the backend and
	// drops the key with it. Those two defers are therefore the whole unlock, and they run on
	// every return — the guarantee this point always made, "an unreleased one would go when
	// the connection goes", minus the statement that could only ever be refused. A run that
	// applied all its files hands the key back when its backend exits, after Migrate has
	// returned, and the only returns that reach the defers early are the caller having
	// cancelled or a file having failed. No probe takes the statement's place: every method on
	// a *sql.Conn, PingContext included, reads the connection's `done` before it takes
	// `closemu`, so a Close completing in between answers a nil driver connection and a nil
	// error — and ending the session is what a session that may be gone answers. The window an
	// `autocommit` file opens *inside* a run is the different case, and it still puts the key
	// down with a statement: mid-run the session is the run's own, and the statement below it
	// must wait for the transactions already in the database. See stepAwayFromTheCompositionLock.
	if err := run.holdCompositionLock(ctx); err != nil {
		return err
	}
	if err := createOwnTables(ctx, conn); err != nil {
		return err
	}
	if err := revokeOwnTables(ctx, conn); err != nil {
		return err
	}
	if err := adoptHistory(ctx, conn, adoptions); err != nil {
		return err
	}
	pending, history, err := pendingMigrations(ctx, conn, migrations)
	if err != nil {
		return err
	}
	if err := checkPendingGuards(pending); err != nil {
		return fmt.Errorf("db: migrate: %w", err)
	}
	for _, group := range groupedByOwner(pending) {
		plan, err := planOwner(ctx, conn, group, history)
		if err != nil {
			return fmt.Errorf("db: migrate: %w", err)
		}
		for _, migration := range plan {
			started := time.Now()
			report, err := run.apply(ctx, migration)
			if err != nil {
				return fmt.Errorf("db: migrate: %s/%s: %w", migration.owner, migration.name, run.refused(err))
			}
			// Info, not Debug, and with the runner's own measurement on it. A release
			// asks which files this run applied and how long each one took, and the
			// rehearsal (scripts/rehearse_migrations.sh) reports the duration the runner
			// measured rather than one a watcher estimated around the process. A data
			// file carries its batch count beside that, because three transactions and
			// fifty at the same duration are two different costs; a schema file has one
			// transaction and no such number to report.
			fields := []any{"owner", migration.owner, "version", migration.version,
				"name", migration.name, "phase", migration.phase}
			if migration.phase == phaseData {
				fields = append(fields, "batches", report.batches)
			}
			slog.InfoContext(ctx, "db: applied migration", append(fields, "duration_ms", time.Since(started).Milliseconds())...)
		}
	}
	return nil
}
