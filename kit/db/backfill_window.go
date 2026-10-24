package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

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

// drainPos holds the last committed key, or an unset cursor before the first
// window. The separate set bit preserves an empty string as a valid text key;
// the ledger represents an unset cursor with NULL.
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
// words for what a blank cursor used to leave a reader to guess. An empty key is rendered
// rather than left blank — a log line that shows nothing beside `cursor` carries the same
// ambiguity the ledger column just gave up.
func (p drainPos) text() string {
	if !p.set {
		return "no window committed"
	}
	if p.key == "" {
		return `""`
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

// window counts the next batch and returns its top key in the type's text
// encoding for resumption. Ordering works for uuid and bytea without a max
// aggregate. An empty window means the table is drained.
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

// windowedBody wraps the owner's statement in the measured, parameterized window.
// An existing CTE list joins that window, retaining RECURSIVE at the front;
// drain has already refused a conflicting batch name. A trailing semicolon
// is removed so the result remains one statement.
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
