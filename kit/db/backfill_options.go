package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// BackfillWithOptions drains pending data migrations under opts' budgets and
// declaration, stopping before an unapplied schema file. It resumes committed
// cursors and answers ErrBackfillBudget at the worker's batch bound, preserving
// completed batches; cursor compare-and-set protects each batch without holding
// the composition lock throughout the drain. opts is the pair MigrateWithOptions
// takes and its zero value is what Backfill runs on. See README.md for the worker
// lifecycle.
//
// The declaration belongs here and not only on the migration: a drain connects of
// its own and runs each window in a transaction of its own, while the declaration a
// file reads is session state — so a placement a boot declared for the migration is
// placed by nothing on the door that drains it, which is where planOwner leaves a
// data file once its owner has history. A door that declares nothing places nothing,
// which is what a file's own refusal is for.
func BackfillWithOptions(ctx context.Context, migrateURL string, opts MigrateOptions, sources ...MigrationSource) error {
	if err := opts.Budget.validate(); err != nil {
		return fmt.Errorf("db: backfill: %w", err)
	}
	migrations, _, err := readMigrations(sources)
	if err != nil {
		return err
	}
	pool, err := sql.Open("pgx", migrateURL)
	if err != nil {
		return fmt.Errorf("db: backfill: open: %w", err)
	}
	defer pool.Close()
	conn, err := pool.Conn(ctx)
	if err != nil {
		return fmt.Errorf("db: backfill: connect: %w", err)
	}
	defer conn.Close()
	if err := requireLedger(ctx, conn); err != nil {
		return err
	}
	// Session state, on the connection every window of this run will take: the same
	// three settings, read the same way, declared before the ledger is read.
	if err := opts.Declaration.declare(ctx, conn); err != nil {
		return fmt.Errorf("db: backfill: %w", err)
	}
	pending, _, err := pendingMigrations(ctx, conn, migrations)
	if err != nil {
		return err
	}
	// The same guard the migration runs: a drain is a second door to the same file,
	// and a floor that excuses nothing excuses less on the door that never reads the
	// rule table than on the one that does.
	if err := checkPendingGuards(pending); err != nil {
		return fmt.Errorf("db: backfill: %w", err)
	}
	run := &runner{conn: conn, budget: opts.Budget}
	for _, group := range groupedByOwner(pending) {
		for _, m := range group {
			if m.phase != phaseData {
				break // the release has schema pending; the deploy migrates, then this drains
			}
			started := time.Now()
			report, err := run.drain(ctx, m, workerBackfillBatches)
			if err != nil {
				return fmt.Errorf("db: backfill: %s/%s: %w", m.owner, m.name, run.refused(err))
			}
			// The drain's own measurement, in the same shape as the per-file line above:
			// the rehearsal reports what the runner timed, and a backfill is the one
			// migration a release most wants the length of. The batch count travels with
			// it, because a file that took three transactions and one that took fifty
			// have the same duration and a different cost per row.
			slog.InfoContext(ctx, "db: drained data migration",
				"owner", m.owner, "version", m.version, "name", m.name, "table", m.table,
				"batch", m.batch, "batches", report.batches, "duration_ms", time.Since(started).Milliseconds())
		}
	}
	return nil
}
