package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// Data migrations drain primary-key windows in separate transactions. Each batch
// commits its cursor with its rows; the last replaces progress with applied history.
// The window reader preserves key encoding and refuses direct writes that move
// its key set. See README.md for lifecycle, budgets and lexical limits.

// ErrBackfillBudget is the bound on a drain one run performs: the fifty windows a migration
// gives itself, or the bound one tick of the worker gives itself (workerBackfillBatches). Past
// either the run is open longer than what it is for, and the rest belongs to the next run. The
// report is one error because the promise is one promise: the committed batches and the cursor
// stand, and the batch count the sentence carries is the bound that stopped the drain.
var ErrBackfillBudget = errors.New("refusal " + refusalBackfillBudget + ": the drain stopped at the bound an installation gives itself; the committed batches stand and the rest is the worker's to drain, then migrate again")

// drain runs one pending data file to completion, or to the bound, or to the first
// error, whichever comes first. Each window is its own transaction, so any of the
// three leaves behind exactly the rows that committed.
func (r *runner) drain(ctx context.Context, m migration, bound int) (drainReport, error) {
	// Refused before anything of this file is written: a progress row means "this
	// drain started, resume it", and a body that can never run is not a drain that
	// started. Every other refusal of a file's shape is the rule table's and happens
	// before the runner connects; this one is the executor's, because it is the
	// window that makes a data file one statement, and the window is written here.
	//
	// The count is taken of the split PostgreSQL makes, not of the one the rule table
	// reads a dollar body inside: a semicolon inside a value the body is writing leaves
	// the body one statement, and the window wraps it whole. This refusal has no marker,
	// so the reading behind it has to be right — a file it refuses has no remedy, and
	// "split the file" cannot split a file that is already one statement.
	if m.windowed() {
		if len(splitServerStatements(m.plain)) > 1 {
			return drainReport{}, fmt.Errorf("a data file is one statement: the window wraps the body, and a second statement would be run over a window of its own with no cursor between them; split the file")
		}
		// The same sentence covers the other body the window cannot wrap: one that binds
		// the window's own name. The wrapper carries a body's own CTE list (see
		// windowedBody), so the file that defines `batch` itself is not a shape the window
		// refuses to run but one whose rows the drain would never advance — PostgreSQL
		// gives two CTEs of one name to nobody at all, and the error arrives after the
		// progress row, which is the state this check exists to leave uncreated.
		if bindsWindowRelation(m.body) {
			return drainReport{}, fmt.Errorf("the window is the relation named %s: this body's own CTE list binds %s, so it reads its own rows and the cursor would advance over a window nothing read; name the body's own relation something else and let the drain supply the window", windowRelation, windowRelation)
		}
	}
	conn := r.conn
	var report drainReport
	key, err := primaryKey(ctx, conn, m.table)
	if err != nil {
		return report, err
	}
	// The third body the window cannot bound, and the one that could only be read here: the
	// cursor is a key, so everything the drain concludes from it — that a short window is the
	// end, that a resumed cursor has no row behind it, that a row is written once — holds of a
	// key set that stays where it was. A body that writes the column the window runs over puts
	// its own rows back above the cursor, and a body that adds rows to the drained table puts
	// keys above it that no window ordered yet; both leave the table never empty of work, and
	// the run rewrites rows an earlier window committed until the bound stops it. Which column
	// the cursor runs over is a fact of the catalog and not of the text, which is why this
	// refusal is the drain's and not the rule table's; it is asked before beginDrain for the
	// same reason the two above are.
	if m.windowed() {
		if move := movesTheWindowKey(m.body, m.table, key.column); move.found() {
			return drainReport{}, fmt.Errorf("the window runs over %s's key %s: %s; %s",
				m.table, key.column, move.wrote(), move.instead())
		}
	}
	if err := beginDrain(ctx, conn, m); err != nil {
		return report, err
	}
	progress := drainCursor(ctx, conn, m.migrationID)
	if progress.err != nil {
		return report, progress.err
	}
	cursor := progress.at
	for {
		if report.batches >= bound {
			return report, fmt.Errorf("%w (owner %s version %d, %d batches of %d, cursor %s)",
				ErrBackfillBudget, m.owner, m.version, report.batches, m.batch, cursor.text())
		}
		// Re-asserted per batch: db.Backfill drains on a tick with no file around it,
		// and a batch with no budget waits forever for a row a request is holding.
		if err := r.budgets(ctx); err != nil {
			return report, drainCutOff(ctx, err)
		}
		done, rows, err := drainWindow(ctx, conn, m, key, &cursor)
		if err != nil {
			return report, drainCutOff(ctx, err)
		}
		if rows > 0 {
			report.batches++
		}
		if done {
			return report, nil
		}
	}
}

// drainCutOff puts the reason a run ended in front of what its session was left holding.
// The caller's context is the only bound one tick of the drain has, so a batch interrupted
// by its end is cut off rather than broken — and the run cannot say so in its own words,
// because neither half of the driver answers a cut-off session with the cut: database/sql
// rolls the begun transaction back, so the next call finds its transaction gone, and pgx
// puts the run's deadline on the socket, so a write in flight answers a network timeout of
// its own. Six of 120 calls of one drain cut off at a random moment reported only what they
// were left holding — a rolled-back transaction, a bad connection, a socket timeout — and
// none of the three names the cut, so each reads as a failed migration to a caller bounding
// a drain by context. The reason leads and the symptom follows, both in the chain: what a
// body genuinely failed on is never lost, and `errors.Is` answers either half. Only the
// drain's per-batch work is translated — ErrBackfillBudget and the refusals of a file's
// shape, a missing table or an unrunnable cursor are reached before a batch begins, which is
// why the two call sites above are the whole of it.
func drainCutOff(ctx context.Context, err error) error {
	if err == nil || ctx.Err() == nil {
		return err
	}
	return fmt.Errorf("%w: the drain was still running when this run's context ended: %w", ctx.Err(), err)
}

// drainReport is what a drain says about itself when it is over: the batches that
// committed work. The runner logs them, because three batches and fifty at the same
// duration are two different releases, and nothing outside the loop can tell them
// apart. Where the drain got to is not in this record: every batch logs its own cursor
// as it commits (batchReported), and the one run that stops before the end names the
// cursor in the error it returns, which are the two moments anyone reads it.
type drainReport struct {
	batches int
}

// batchReported is the drain's own progress line, at debug: which table is being
// drained, how far the batch that just committed got, and how long it took. A drain
// that stops halfway through a table otherwise reports its position only as a row in
// schema_migration_backfill, which is where the next run looks and not where an
// operator watching one does.
func batchReported(ctx context.Context, m migration, rows int64, cursor drainPos, started time.Time) {
	slog.DebugContext(ctx, "db: backfill batch", "owner", m.owner, "version", m.version, "name", m.name,
		"table", m.table, "rows", rows, "cursor", cursor.text(), "duration_ms", time.Since(started).Milliseconds())
}

// finishDrain is the moment a data migration becomes an applied one: the history
// row that says it ran, and the progress row that said where it had got to, in the
// one transaction that moves between the two.
func finishDrain(ctx context.Context, tx *sql.Tx, m migration) error {
	if err := recordIn(ctx, tx, m); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "DELETE FROM schema_migration_backfill WHERE owner = $1 AND version = $2", m.owner, m.version)
	return err
}

// Backfill drains pending data migrations with default budgets, stopping before
// an unapplied schema file. It resumes committed cursors and returns
// ErrBackfillBudget at the worker's batch bound, preserving completed batches.
// Cursor compare-and-set protects each batch without holding the composition
// lock throughout the drain. See README.md for the worker lifecycle.
//
// Deprecated: Use BackfillWithOptions with the zero MigrateOptions, which drains
// on these same default budgets and declares nothing.
func Backfill(ctx context.Context, migrateURL string, sources ...MigrationSource) error {
	return BackfillWith(ctx, migrateURL, MigrationBudget{}, sources...)
}

// BackfillWith is Backfill with the budgets a deployment named in its configuration,
// re-asserted on the runner's session before every batch exactly as MigrateWith does
// before every file. The drain is the longer half of a release — fifty transactions of
// waiting for rows the running application is holding, where a file is one — so the
// patience an operator shortened because a boot must not sit on a busy table is the
// one that matters most here, and a drain that always ran on the default would run the
// work on a patience its own deployment had refused.
//
// Deprecated: Use BackfillWithOptions with MigrateOptions{Budget: budget}, which is
// this call and nothing more.
func BackfillWith(ctx context.Context, migrateURL string, budget MigrationBudget, sources ...MigrationSource) error {
	return BackfillDeclaring(ctx, migrateURL, budget, Declaration{}, sources...)
}

// BackfillDeclaring is BackfillWith plus what the boot declares about itself, the
// same three settings the migration puts on its session; see BackfillWithOptions
// for why a drain needs them on its own connection.
//
// Deprecated: Use BackfillWithOptions, which takes the budget and the declaration
// as the one MigrateOptions it runs on.
func BackfillDeclaring(ctx context.Context, migrateURL string, budget MigrationBudget, decl Declaration, sources ...MigrationSource) error {
	return BackfillWithOptions(ctx, migrateURL, MigrateOptions{Budget: budget, Declaration: decl}, sources...)
}

// requireLedger is the drain's precondition: both tables it writes already exist.
// Creating them is the migration's job, not this one's — the drain holds no advisory
// lock, and DDL outside that lock waits behind a migration or cuts across one. A worker
// started before the deploy step has nothing drained, says so, and ticks again after
// it; no drain that could do work is refused here, because the same run that could have
// a data file pending is the run that creates these tables.
func requireLedger(ctx context.Context, conn *sql.Conn) error {
	var ledger, progress string
	if err := conn.QueryRowContext(ctx, `SELECT coalesce(to_regclass('schema_migrations')::text, ''),
		coalesce(to_regclass('schema_migration_backfill')::text, '')`).Scan(&ledger, &progress); err != nil {
		return fmt.Errorf("db: backfill: look for the runner's tables: %w", err)
	}
	if ledger == "" || progress == "" {
		return fmt.Errorf("db: backfill: this database has no schema_migrations or schema_migration_backfill to resume a drain from; the deploy step migrates, then the drain runs")
	}
	return nil
}
