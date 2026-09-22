package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// A phase=data file is the one migration that runs outside one transaction, and the
// reason is size: a backfill over a ten-million-row table is a rewrite of the table if
// it is one statement, and a rewrite holds ACCESS EXCLUSIVE while the application keeps
// trying to read the rows it is copying.
//
// So the body never runs as written. It is wrapped over a window of the table's own
// primary key,
//
//	WITH batch AS (SELECT id FROM probe WHERE id > $1::bigint ORDER BY id LIMIT 5000)
//	UPDATE probe SET … WHERE id IN (SELECT id FROM batch)
//
// and run once per window, one committed transaction per window. "One statement" is
// structural rather than checked: the kernel writes the window, so a body cannot escape
// it without saying so out loud. Progress lives in schema_migration_backfill as the last
// key the run committed — a key, not an offset, so editing the body of a file that never
// applied cannot make it repeat or skip — and the row and the history row are never both
// present: the drain commits the delete of one and the insert of the other in the
// transaction that found the last window empty.

// ErrBackfillBudget is the bound on a drain a migration run performs for itself: past it
// the process is open too long, and the rest belongs to the worker, which can drain a
// table under readers. Committed batches and the cursor stand.
var ErrBackfillBudget = errors.New("db: backfill stopped at the bound an installation gives itself; the committed batches stand and the rest is the worker's to drain, then migrate again")

// drain runs one pending data file to completion, or to the bound, or to the first
// error, whichever comes first. Each window is its own transaction, so any of the
// three leaves behind exactly the rows that committed.
func (r *runner) drain(ctx context.Context, m migration, bound int) error {
	conn := r.conn
	key, err := primaryKey(ctx, conn, m.table)
	if err != nil {
		return err
	}
	if err := beginDrain(ctx, conn, m); err != nil {
		return err
	}
	if m.windowed() && len(splitStatements(strings.ToLower(m.body))) > 1 {
		return fmt.Errorf("a data file is one statement: the window wraps the body, and a second statement would be run over a window of its own with no cursor between them; split the file")
	}
	progress := drainCursor(ctx, conn, m.migrationID)
	if progress.err != nil {
		return progress.err
	}
	cursor := progress.cursor
	for batches := 0; ; batches++ {
		if bound > 0 && batches >= bound {
			return fmt.Errorf("%w (owner %s version %d, %d batches of %d, cursor %s)",
				ErrBackfillBudget, m.owner, m.version, batches, m.batch, cursor)
		}
		// Re-asserted per batch: db.Backfill drains on a tick with no file around it,
		// and a batch with no budget waits forever for a row a request is holding.
		if err := r.budgets(ctx); err != nil {
			return err
		}
		done, err := drainWindow(ctx, conn, m, key, &cursor)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
}

// drainWindow is one batch: the window measured, the body run over it, and the
// cursor moved to the last key of it — all in one transaction, which is the whole
// claim the batch job exists for. An empty window is the end of the table, and the
// history row and the progress row change hands inside that same transaction.
func drainWindow(ctx context.Context, conn *sql.Conn, m migration, key tableKey, cursor *string) (bool, error) {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if err := crossTenants(ctx, tx); err != nil {
		return false, err
	}
	if !m.windowed() {
		// The file excepted data-body-unbounded, which is its owner saying the
		// body bounds itself. It runs once, in this transaction, and the drain is
		// over: there is no window to resume, and nothing to pretend otherwise.
		if _, err := tx.ExecContext(ctx, m.body); err != nil {
			return false, err
		}
		if err := finishDrain(ctx, tx, m); err != nil {
			return false, err
		}
		return true, tx.Commit()
	}
	rows, top, err := window(ctx, tx, m, key, *cursor)
	if err != nil {
		return false, err
	}
	if rows == 0 {
		if err := finishDrain(ctx, tx, m); err != nil {
			return false, err
		}
		return true, tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, m.windowedBody(key, *cursor), windowArgs(*cursor)...); err != nil {
		return false, err
	}
	if err := advanceCursor(ctx, tx, m, *cursor, top); err != nil {
		return false, err
	}
	*cursor = top
	return false, tx.Commit()
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

// Backfill finishes the data migrations an installation has left unfinished, as
// far as it can without touching schema. It is the worker's door, composed as
// jobs.BackfillMigrations: Migrate stops in front of a drain over a table with
// readers, because the process that drains one is this one, and a boot that
// refused would stop the only role that can finish the work.
//
// It takes the files in order and stops an owner's walk at the first unapplied
// schema file: a backfill runs against the schema the release installed, so the
// deploy step goes first and the drain follows it. A file with a progress row
// resumes at its cursor; one without starts at the first key.
//
// It deliberately does not take the advisory lock every migration takes for its
// whole composition: a drain that held that lock for the length of a ten million
// row table would stop every other role's boot behind it, and the write that
// protects a drain from a second runner is the compare-and-set on the cursor
// rather than a lock held across the work.
func Backfill(ctx context.Context, migrateURL string, sources ...MigrationSource) error {
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
	pending, _, err := pendingMigrations(ctx, conn, migrations)
	if err != nil {
		return err
	}
	run := &runner{conn: conn, budget: DefaultMigrationBudget()}
	for _, group := range groupedByOwner(pending) {
		for _, m := range group {
			if m.phase != phaseData {
				break // the release has schema pending; the deploy migrates, then this drains
			}
			if err := run.drain(ctx, m, 0); err != nil {
				return fmt.Errorf("db: backfill: %s/%s: %w", m.owner, m.name, run.refused(err))
			}
			slog.InfoContext(ctx, "db: drained data migration",
				"owner", m.owner, "version", m.version, "name", m.name, "table", m.table, "batch", m.batch)
		}
	}
	return nil
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

// tableKey is the column a resumable window runs over: the table's own primary key,
// read from the catalog rather than declared in the header, because the header
// could claim a key the table does not have.
type tableKey struct {
	column string
	pgType string
}

const primaryKeySQL = `
	SELECT a.attname, format_type(a.atttypid, a.atttypmod)
	FROM pg_index i
	JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = i.indkey[0]
	WHERE i.indrelid = to_regclass($1) AND i.indisprimary AND i.indnatts = 1`

const tableExistsSQL = "SELECT to_regclass($1) IS NOT NULL"

func primaryKey(ctx context.Context, conn *sql.Conn, table string) (tableKey, error) {
	var key tableKey
	err := conn.QueryRowContext(ctx, primaryKeySQL, table).Scan(&key.column, &key.pgType)
	if err == nil {
		return key, nil
	}
	if err != sql.ErrNoRows {
		return key, fmt.Errorf("reading %s's primary key: %w", table, err)
	}
	var exists bool
	if err := conn.QueryRowContext(ctx, tableExistsSQL, table).Scan(&exists); err != nil {
		return key, fmt.Errorf("reading %s: %w", table, err)
	}
	if !exists {
		return key, fmt.Errorf("table %s named by a data migration does not exist here; it belongs to a file of another owner that has not been selected, or to a release that has not applied yet", table)
	}
	return key, fmt.Errorf("table %s has no single-column primary key, which is what a window that can resume by key needs; a table keyed by something else needs a drain its owner owns, in a job", table)
}

// beginDrain gives this file a progress row to advance, without moving one that is
// already there: the row a previous run left is the place this run starts from.
func beginDrain(ctx context.Context, conn *sql.Conn, m migration) error {
	_, err := conn.ExecContext(ctx,
		"INSERT INTO schema_migration_backfill (owner, version) VALUES ($1, $2) ON CONFLICT (owner, version) DO NOTHING",
		m.owner, m.version)
	return err
}

// drainProgress is the cursor as the ledger of an unfinished drain holds it: no
// row at all before the first window, empty after it, and then the last key the
// run committed.
type drainProgress struct {
	cursor  string
	started bool
	err     error
}

func drainCursor(ctx context.Context, conn *sql.Conn, id migrationID) drainProgress {
	var p drainProgress
	err := conn.QueryRowContext(ctx,
		"SELECT cursor FROM schema_migration_backfill WHERE owner = $1 AND version = $2",
		id.owner, id.version).Scan(&p.cursor)
	switch {
	case err == sql.ErrNoRows:
		return drainProgress{}
	case err != nil:
		p.err = fmt.Errorf("reading the backfill cursor for %s/%d: %w", id.owner, id.version, err)
	default:
		p.started = true
	}
	return p
}

// advanceCursor moves the drain forward on the condition that it is where this run
// left it. The advisory lock is the first barrier against a second runner; this is
// the honest one, because it is checked against the row rather than against a
// connection. Zero rows means somebody else is ahead: this batch rolls back and
// stops, and nothing double-writes.
func advanceCursor(ctx context.Context, tx *sql.Tx, m migration, from, to string) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE schema_migration_backfill SET cursor = $3, updated_at = clock_timestamp()
		WHERE owner = $1 AND version = $2 AND cursor = $4`, m.owner, m.version, to, from)
	if err != nil {
		return err
	}
	if moved, _ := result.RowsAffected(); moved == 1 {
		return nil
	}
	return fmt.Errorf("another runner took the backfill of %s/%d ahead of this one; this batch is rolled back and the drain continues from where it committed", m.owner, m.version)
}

// window counts the next batch and returns the highest key in it, rendered by its
// own type so that it is the same string the next run reads back. An empty window
// is the ordinary end of the table, not a failure.
func window(ctx context.Context, tx *sql.Tx, m migration, key tableKey, from string) (int64, string, error) {
	query := fmt.Sprintf(
		`SELECT count(*), coalesce(max(w)::text, '') FROM (SELECT %s AS w FROM %s WHERE %s ORDER BY w LIMIT %d) windowed`,
		quoteIdentifier(key.column), quoteIdentifier(m.table), windowBound(key, from), m.batch)
	var rows int64
	var top string
	err := tx.QueryRowContext(ctx, query, windowArgs(from)...).Scan(&rows, &top)
	if err != nil {
		return 0, "", fmt.Errorf("measuring the next batch of %s: %w", m.table, err)
	}
	return rows, top, nil
}

// windowedBody is the owner's body with the kernel's window around it: the same
// bound the measurement just took, so what runs is what was counted. The cursor is
// a parameter, never text in the statement, and the body's trailing semicolon goes
// away because the window makes the two one statement.
func (m migration) windowedBody(key tableKey, from string) string {
	return fmt.Sprintf("WITH batch AS (SELECT %s FROM %s WHERE %s ORDER BY %s LIMIT %d)\n%s",
		quoteIdentifier(key.column), quoteIdentifier(m.table), windowBound(key, from),
		quoteIdentifier(key.column), m.batch, strings.TrimRight(m.body, " \n\t;"))
}

// windowBound is the half-open window (cursor, …] the drain walks. The first
// window has no lower bound at all: there is no value of every possible key type
// that is below every other one, so the run says "from the start" instead of
// inventing a minimum and losing the first row with it.
func windowBound(key tableKey, from string) string {
	if from == "" {
		return "TRUE"
	}
	return fmt.Sprintf("%s > $1::%s", quoteIdentifier(key.column), key.pgType)
}

func windowArgs(from string) []any {
	if from == "" {
		return nil
	}
	return []any{from}
}

// windowed says whether this body goes through a window at all. A file that
// excepted data-body-unbounded does not: its owner said the body bounds itself, and
// the honest reading of that is one statement, run once, in one transaction.
func (m migration) windowed() bool {
	return reBatchWindow.MatchString(m.body)
}

// crossTenants is the one place a migration reaches every tenant's rows, and it
// says so in the way scripts/check_gucs.sh reads: a drain that walked one tenant at
// a time would need a tenant list, which a migration must not have, and one that
// simply ran as the table owner would be refused by the FORCE every tenant table
// carries. The setting is transaction-local: this batch, and no other.
func crossTenants(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `SELECT set_config('platformkit.system_access', 'true', true)`); err != nil {
		return fmt.Errorf("opening a cross-tenant batch: %w", err)
	}
	return nil
}

// quoteIdentifier wraps one name the way the SQL's own writers would. The header
// grammar already refuses anything but a bare lower-case identifier, so this is
// belt-and-braces over a value that came out of the catalog or a checked header.
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
