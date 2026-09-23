package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
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
// transaction that wrote its last window, so the moment the table is empty of work is
// the moment the ledger says the version applied.
//
// Two things the window has to survive are stated with the code rather than assumed:
// a key the server can order but not `max` (the top of a window is taken the same way the
// window is, so `uuid` and `bytea` — the types every entity table here is keyed by — are
// windows and not server errors), and a key that is also the sentinel ("nothing committed
// yet" is a NULL cursor, because for a `text` key the empty string is a row).

// ErrBackfillBudget is the bound on a drain a migration run performs for itself: past it
// the process is open too long, and the rest belongs to the worker, which can drain a
// table under readers. Committed batches and the cursor stand.
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
		if reWindowShadowed.MatchString(m.shape) {
			return drainReport{}, fmt.Errorf("the window is the relation named batch: this body defines batch itself, so it reads its own rows and the cursor would advance over a window nothing read; name the body's own relation something else and let the drain supply the window")
		}
	}
	conn := r.conn
	var report drainReport
	key, err := primaryKey(ctx, conn, m.table)
	if err != nil {
		return report, err
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
		if bound > 0 && report.batches >= bound {
			return report, fmt.Errorf("%w (owner %s version %d, %d batches of %d, cursor %s)",
				ErrBackfillBudget, m.owner, m.version, report.batches, m.batch, cursor.text())
		}
		// Re-asserted per batch: db.Backfill drains on a tick with no file around it,
		// and a batch with no budget waits forever for a row a request is holding.
		if err := r.budgets(ctx); err != nil {
			return report, err
		}
		done, rows, err := drainWindow(ctx, conn, m, key, &cursor)
		if err != nil {
			return report, err
		}
		if rows > 0 {
			report.batches++
		}
		if done {
			return report, nil
		}
	}
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

// drainWindow is one batch: the window measured, the body run over it, the cursor
// moved to the last key of it, and the end of the table asked about — all in one
// transaction, which is the whole claim the batch job exists for. It reports whether
// the table is at its end and how many rows the batch wrote, which is what the runner
// counts and logs. A window that came back short, or an empty one where a finished
// cursor was resumed, is the end, and the history row and the progress row change hands
// inside the transaction that wrote the work rather than in one after it.
func drainWindow(ctx context.Context, conn *sql.Conn, m migration, key tableKey, cursor *drainPos) (done bool, rows int64, err error) {
	started := time.Now()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return false, 0, err
	}
	defer tx.Rollback()
	if err := crossTenants(ctx, tx); err != nil {
		return false, 0, err
	}
	if !m.windowed() {
		// The file excepted data-body-unbounded, which is its owner saying the body
		// bounds itself. The one reading that decided this is the reading the rule
		// table made — the body with its comments gone and its case folded — so the
		// file that was judged is the file that runs. It runs once, in this
		// transaction, and the drain is over: there is no window to resume, and
		// nothing to pretend otherwise.
		result, err := tx.ExecContext(ctx, m.body)
		if err != nil {
			return false, 0, err
		}
		rows, _ = result.RowsAffected()
		if err := finishDrain(ctx, tx, m); err != nil {
			return false, 0, err
		}
		if err := tx.Commit(); err != nil {
			return false, 0, err
		}
		batchReported(ctx, m, rows, *cursor, started)
		return true, rows, nil
	}
	measured, top, err := window(ctx, tx, m, key, *cursor)
	if err != nil {
		return false, 0, err
	}
	if measured == 0 {
		// A cursor that had already reached the end — the run that resumes a drain whose
		// last batch committed somewhere else — and the fresh drain over an empty table.
		// Nothing to write, and the hand-over is this transaction's too.
		if err := finishDrain(ctx, tx, m); err != nil {
			return false, 0, err
		}
		return true, 0, tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, m.windowedBody(key, *cursor), cursor.args()...); err != nil {
		return false, 0, err
	}
	if err := advanceCursor(ctx, tx, m, *cursor, top); err != nil {
		return false, 0, err
	}
	// The end is measured here, in the transaction that wrote the last window, and not
	// in one after it. A run that wrote every row and stopped before its next
	// transaction would say "work left" from the two tables while there was none, and
	// nothing outside the loop can tell that state from a drain that truly stopped
	// halfway: the next plan resumes it, and the release behind it waits for a cursor
	// with no rows behind it. So "every row written" and "the version applied" are one
	// commit, which is the only fact the two tables can agree on.
	end, err := drainEnds(ctx, tx, m, key, top, measured)
	if err != nil {
		return false, 0, err
	}
	if end {
		if err := finishDrain(ctx, tx, m); err != nil {
			return false, 0, err
		}
		if err := tx.Commit(); err != nil {
			return false, 0, err
		}
		*cursor = top
		batchReported(ctx, m, measured, top, started)
		return true, measured, nil
	}
	if err := tx.Commit(); err != nil {
		return false, 0, err
	}
	*cursor = top
	batchReported(ctx, m, measured, top, started)
	return false, measured, nil
}

// drainEnds asks whether the table holds any key after this batch. A window that came
// back short already answered it: the measurement is `ORDER BY key LIMIT batch` over
// everything above the cursor, in this transaction's snapshot, and fewer rows than the
// limit means nothing is left above that key. A full window has to look at the next
// one, which is the measurement the next batch would have taken — and the one this run
// would otherwise have taken in a transaction after the last batch committed, so the
// drain counts its windows the same number of times and spends one fewer transaction.
func drainEnds(ctx context.Context, tx *sql.Tx, m migration, key tableKey, top drainPos, measured int64) (bool, error) {
	if measured < int64(m.batch) {
		return true, nil
	}
	left, _, err := window(ctx, tx, m, key, top)
	return left == 0, err
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

// Backfill finishes the data migrations an installation has left unfinished, as
// far as it can without touching schema, on the documented budgets. It is the
// worker's door, composed as jobs.BackfillMigrations: Migrate stops in front of a
// drain over a table with readers, because the process that drains one is this one,
// and a boot that refused would stop the only role that can finish the work. It is
// BackfillWith with no budget named, the same pair as Migrate and MigrateWith.
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
	return BackfillWith(ctx, migrateURL, MigrationBudget{}, sources...)
}

// BackfillWith is Backfill with the budgets a deployment named in its configuration,
// re-asserted on the runner's session before every batch exactly as MigrateWith does
// before every file. The drain is the longer half of a release — fifty transactions of
// waiting for rows the running application is holding, where a file is one — so the
// patience an operator shortened because a boot must not sit on a busy table is the
// one that matters most here, and a drain that always ran on the default would run the
// work on a patience its own deployment had refused.
func BackfillWith(ctx context.Context, migrateURL string, budget MigrationBudget, sources ...MigrationSource) error {
	if err := budget.validate(); err != nil {
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
	run := &runner{conn: conn, budget: budget}
	for _, group := range groupedByOwner(pending) {
		for _, m := range group {
			if m.phase != phaseData {
				break // the release has schema pending; the deploy migrates, then this drains
			}
			started := time.Now()
			report, err := run.drain(ctx, m, 0)
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
		return key, refusal(refusalTableMissing,
			fmt.Sprintf("table %s named by a data migration does not exist here", table),
			"it belongs to a file of another owner that has not been selected, or to a release that has not applied yet")
	}
	return key, refusal(refusalKeyNotPrimaryKey,
		fmt.Sprintf("table %s has no single-column primary key, which is what a window that can resume by key needs", table),
		"a table keyed by something else needs a drain its owner owns, in a job")
}

// beginDrain gives this file a progress row to advance, without moving one that is
// already there: the row a previous run left is the place this run starts from.
func beginDrain(ctx context.Context, conn *sql.Conn, m migration) error {
	_, err := conn.ExecContext(ctx,
		"INSERT INTO schema_migration_backfill (owner, version) VALUES ($1, $2) ON CONFLICT (owner, version) DO NOTHING",
		m.owner, m.version)
	return err
}

// drainPos is a drain's place in its table's own key order: the last key a window
// committed, or the fact that no window has committed yet.
//
// Those are two facts, and one string cannot hold both of them. The cursor column renders a
// key as text, and for a `text` or `varchar` primary key — which is how `tenant_hosts.host`
// and `platformkit_limits.key` are keyed — the empty string is a legal key and the smallest
// one in every collation. A column that is NOT NULL and defaults to the empty string said
// "nothing committed" and "the row keyed by the empty string committed" with one value, so a
// drain whose first window topped out at that key wrote the cursor back to the string the run
// started with, the window above it (`host > $1::text`, bound to that same empty string) took
// the rows behind it and stopped at the same top, and the release never
// converged: `Migrate` to its batch bound rewriting one row as fast as it could commit, then
// `jobs.BackfillMigrations` rewriting the same row on every tick forever, with the rows that
// were the point of the backfill never reached. The ledger holds "nothing committed" as a
// NULL now; the window has always held it as no lower bound at all.
type drainPos struct {
	key string
	set bool
}

// atKey is the position after a window that came back with a top: the key itself, whatever
// it is, and the flag that says a key is what it is.
func atKey(key string) drainPos { return drainPos{key: key, set: true} }

// value is the cursor column's value for this position — the key, or NULL, which is the one
// spelling of "from the start" that is not also somebody's primary key — or the parameter
// the window compares against, which is the same value or nothing at all.
func (p drainPos) value() any {
	if !p.set {
		return nil
	}
	return p.key
}

// text is the position as an operator reads it: the key the next run restarts after, or the
// words for what a blank cursor used to leave a reader to guess.
func (p drainPos) text() string {
	if !p.set {
		return "no window committed"
	}
	return p.key
}

// bound is the window's lower edge: every key above the last one committed, and the whole
// table when nothing is committed. There is no value of every possible key type below every
// other one, so the run says "from the start" rather than inventing a minimum and losing the
// first row with it.
func (p drainPos) bound(key tableKey) string {
	if !p.set {
		return "TRUE"
	}
	return fmt.Sprintf("%s > $1::%s", quoteIdentifier(key.column), key.pgType)
}

// args is the parameter bound names, if it names one: the cursor is a parameter and never
// text in the statement.
func (p drainPos) args() []any {
	if !p.set {
		return nil
	}
	return []any{p.key}
}

// drainProgress is the cursor as the ledger of an unfinished drain holds it: no row at all
// before the first window, then a row holding either "no window committed yet" or the last
// key the run committed.
type drainProgress struct {
	at      drainPos
	started bool
	err     error
}

func drainCursor(ctx context.Context, conn *sql.Conn, id migrationID) drainProgress {
	var p drainProgress
	var cursor sql.NullString
	err := conn.QueryRowContext(ctx,
		"SELECT cursor FROM schema_migration_backfill WHERE owner = $1 AND version = $2",
		id.owner, id.version).Scan(&cursor)
	switch {
	case err == sql.ErrNoRows:
		return drainProgress{}
	case err != nil:
		p.err = fmt.Errorf("reading the backfill cursor for %s/%d: %w", id.owner, id.version, err)
	default:
		p.started = true
		if cursor.Valid {
			p.at = atKey(cursor.String)
		}
	}
	return p
}

// advanceCursor moves the drain forward on the condition that it is where this run
// left it. The advisory lock is the first barrier against a second runner; this is
// the honest one, because it is checked against the row rather than against a
// connection. Zero rows means somebody else is ahead: this batch rolls back and
// stops, and nothing double-writes.
//
// `IS NOT DISTINCT FROM` and not `=` because the place this run started from is often "no
// window committed", which the ledger holds as a NULL: `cursor = $4` over a NULL is NULL, no
// row matches it, and the first batch of every fresh drain would report a second runner that
// does not exist.
func advanceCursor(ctx context.Context, tx *sql.Tx, m migration, from, to drainPos) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE schema_migration_backfill SET cursor = $3, updated_at = clock_timestamp()
		WHERE owner = $1 AND version = $2 AND cursor IS NOT DISTINCT FROM $4::text`,
		m.owner, m.version, to.value(), from.value())
	if err != nil {
		return err
	}
	if moved, _ := result.RowsAffected(); moved == 1 {
		return nil
	}
	return fmt.Errorf("another runner took the backfill of %s/%d ahead of this one; this batch is rolled back and the drain continues from where it committed", m.owner, m.version)
}

// window counts the next batch and returns its top key, rendered by the key's own type so
// that it is the same string the next run reads back. An empty window is the ordinary end of
// the table, not a failure.
//
// The top is taken the way the window itself is taken, `ORDER BY w DESC LIMIT 1`, and not
// with `max(w)`, because PostgreSQL has no `max` aggregate for `uuid` or for `bytea` — the
// types `tenants.id`, `users.id`, `platformkit_outbox.id` and every module's entity table are
// keyed by, and the type `modules/auth` keys its token hashes by. Those types are orderable,
// which is the only thing a window needs of a key, and an aggregate that does not exist for
// them answered the first window of every such table with `function max(uuid) does not
// exist`, after the drain had written its progress row. The ordering is asked of the server
// either way, so what the drain can window over stays the whole set of types with an
// ordering operator rather than the list of types with an aggregate; and the rendering
// (`w::text`) is what that type's own input function takes back: measured on the pinned
// server, the top of a `uuid` window comes out in its dashed form and the top of a `bytea`
// window as `\x`-hex, and both re-enter as `$1::uuid` and `$1::bytea` and name the same key.
func window(ctx context.Context, tx *sql.Tx, m migration, key tableKey, from drainPos) (int64, drainPos, error) {
	query := fmt.Sprintf(`WITH windowed AS (SELECT %s AS w FROM %s WHERE %s ORDER BY w LIMIT %d)
		SELECT (SELECT count(*) FROM windowed),
		       coalesce((SELECT w::text FROM (SELECT w FROM windowed ORDER BY w DESC LIMIT 1) top), '')`,
		quoteIdentifier(key.column), quoteIdentifier(m.table), from.bound(key), m.batch)
	var rows int64
	var top string
	if err := tx.QueryRowContext(ctx, query, from.args()...).Scan(&rows, &top); err != nil {
		return 0, drainPos{}, fmt.Errorf("measuring the next batch of %s: %w", m.table, err)
	}
	if rows == 0 {
		return 0, drainPos{}, nil
	}
	return rows, atKey(top), nil
}

// windowedBody is the owner's body with the kernel's window around it: the same bound the
// measurement just took, so what runs is what was counted. The cursor is a parameter, never
// text in the statement, and the body's trailing semicolon goes away because the window
// makes the two one statement.
//
// A body that opens with a CTE list of its own — `WITH stale AS (…) UPDATE …`, which is the
// shape a backfill that names the rows it is about to touch takes naturally — has that list
// joined into *our* `WITH`, because PostgreSQL takes one `WITH` per statement: pasting a
// second one in front of the body answered a file the kernel assembled with the server's own
// syntax error, after the progress row, which leaves a drain forever half-started. The
// window leads the merged list, so a body's own CTE may read it, and `RECURSIVE` is a
// property of a list rather than of one member, so the body's word for it moves to the front
// of the statement. A body that binds the name `batch` itself is refused before any of this
// (drain names it): PostgreSQL answers two CTEs of one name by refusing the statement, and
// the rows such a drain would advance over would be nobody's window.
func (m migration) windowedBody(key tableKey, from drainPos) string {
	window := fmt.Sprintf("SELECT %s FROM %s WHERE %s ORDER BY %s LIMIT %d",
		quoteIdentifier(key.column), quoteIdentifier(m.table), from.bound(key),
		quoteIdentifier(key.column), m.batch)
	body := strings.TrimRight(m.body, " \n\t;")
	if after, recursive, opens := cteLead(body); opens {
		if recursive {
			return "WITH RECURSIVE batch AS (" + window + "),\n" + strings.TrimLeft(body[after:], " \t\n")
		}
		return "WITH batch AS (" + window + "),\n" + strings.TrimLeft(body[after:], " \t\n")
	}
	return "WITH batch AS (" + window + ")\n" + body
}

// cteLead is the offset just past the `WITH` — and the `RECURSIVE` that may follow it — that
// opens a body's statement, whether the body said RECURSIVE, and whether it opened with a CTE
// list at all. Whitespace and commentary are walked with sqlToken, the reader every other
// boundary in this package takes, so a body that opens with a comment is not read as opening
// with the words of the comment. The only statement PostgreSQL opens with the word `with` is
// one carrying a common table expression list, so anything else at the front is an ordinary
// body and the answer says so rather than guessing.
func cteLead(body string) (after int, recursive bool, opens bool) {
	word, next, found := sqlWord(body, 0)
	if !found || word != "with" {
		return 0, false, false
	}
	if word, later, found := sqlWord(body, next); found && word == "recursive" {
		return later, true, true
	}
	return next, false, true
}

// sqlWord reads the next word of text at or after i — commentary and whitespace walked over,
// the word lower-cased the way every other reading of a file's SQL is — and the offset past
// it. A character that starts no word (a parenthesis, a quote the scanner did not take) is an
// answer of false: this is not a word, and the caller reads nothing from it.
func sqlWord(text string, i int) (word string, after int, found bool) {
	for i < len(text) {
		if width := sqlToken(text, i); width > 0 {
			i += width
			continue
		}
		if text[i] == ' ' || text[i] == '\t' || text[i] == '\n' || text[i] == '\r' {
			i++
			continue
		}
		start := i
		for i < len(text) && (text[i] == '_' || text[i] == '$' ||
			(text[i] >= 'a' && text[i] <= 'z') || (text[i] >= 'A' && text[i] <= 'Z') ||
			(text[i] >= '0' && text[i] <= '9')) {
			i++
		}
		return strings.ToLower(text[start:i]), i, i > start
	}
	return "", i, false
}

// windowed says whether this body goes through a window at all. It is the same
// question data-body-unbounded asks, and it is asked once — newMigrationText takes the
// answer from here, so the rule table and the drain cannot ever disagree about one file.
// The question is put to the body's shape rather than its text, because the question is
// about the statement: a body that names the window only inside a value it is writing
// does not read it, and a body wrapped without reading the window would be run once per
// window over every row of the table, which is the thing the rule exists to make
// impossible.
func (m migration) windowed() bool { return reBatchWindow.MatchString(m.shape) }

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
