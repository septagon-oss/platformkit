package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// MigrationSource is the SQL owned by one capability. Owner is a stable name;
// versions increase within that owner, independently of every other capability.
// Files contains <version>_<name>.up.sql files at its root.
type MigrationSource struct {
	Owner string
	Files fs.FS
	// Adopts is history this owner takes over from another: ledger rows the
	// named owner applied whose version and checksum match a file of this
	// source, under the same version number. Migrate re-owns them in one
	// transaction before it reads any history, so the adopted files are
	// neither missing from the new owner nor pending for the new one, and
	// their SQL never runs again. It is how a module takes the files the
	// foundation once applied under its own name (docs/adr/0011); a fresh
	// installation has nothing to adopt and the declaration is a no-op.
	Adopts []Adoption
	// RulesFrom is the first version this source's files are guarded from by the
	// rule table in migration_rules.go. Zero — the value nobody writes — guards
	// every file, which is what a source added after the rules exist wants. A
	// source with history names the version past the last file it already has
	// applied somewhere, because a rule cannot be refused on a file that is
	// already applied: the bytes are immutable, so the only remedy left would be
	// to stop the installation. The floor is the owner's to state, because what
	// it installed is the owner's fact, and kit/app carries it through a module's
	// manifest unchanged.
	//
	// It is bounded by what it can claim: a number past this source's own highest
	// version plus one describes history no release of this source could have
	// applied, so it excuses no file and every file of the source is guarded
	// (docs/adr/0011). A floor is how a source says "these bytes ran"; it is not a
	// way to leave the guard off.
	RulesFrom int64
}

// Adoption names one previous owner and the versions of its ledger rows that
// are now the adopting source's files. Every version must be a file of the
// adopting source; an adopted row whose checksum differs from that file is a
// changed migration and refuses, like any other changed applied file.
type Adoption struct {
	Owner    string
	Versions []int64
}

// Sub is fs.Sub for an embedded migrations directory: the files under dir as a
// source's root, which is where readMigrations looks. It panics where fs.Sub
// would return an error, because dir is a literal written beside a //go:embed
// directive and a wrong one is a mistake with no runtime to report to.
func Sub(fsys fs.FS, dir string) fs.FS {
	files, err := fs.Sub(fsys, dir)
	if err != nil {
		panic("db: Sub: " + err.Error())
	}
	return files
}

// The runner's budgets are its own patience and never the operator's: a migration
// that queues behind the running application must stop and say so within seconds,
// which is the difference between a release that waits and an installation that
// appears to have stopped. defaultLockTimeout is the wait a migration is given for
// a lock — five seconds of waiting, never of holding. It is spelled as PostgreSQL
// spells it because current_setting answers in the text a value was set with, and
// an operator reading the setting back should read what the documentation says.
const defaultLockTimeout = 5 * time.Second

// MigrationBudget is how long one migration file may wait — for a lock, and for a
// statement overall — before the runner stops it, put on the runner's own session
// before every file and every batch. A nil field is the documented default.
//
// The statement budget defaults to no bound deliberately: the wait no installation
// should sit through is the lock wait, and a legitimate index build on a large
// table is the statement a duration bound would kill.
type MigrationBudget struct {
	LockTimeout      *time.Duration
	StatementTimeout *time.Duration
}

// DefaultMigrationBudget is what a run that names no budget uses: five seconds for
// a lock, and no bound on a statement.
func DefaultMigrationBudget() MigrationBudget {
	lock := defaultLockTimeout
	return MigrationBudget{LockTimeout: &lock}
}

// budgets returns the two durations in force, defaults filled in. A zero statement
// duration is PostgreSQL's own "no limit".
func (b MigrationBudget) budgets() (lock, statement time.Duration) {
	lock, statement = defaultLockTimeout, 0
	if b.LockTimeout != nil {
		lock = *b.LockTimeout
	}
	if b.StatementTimeout != nil {
		statement = *b.StatementTimeout
	}
	return lock, statement
}

// statement is the pair of settings. On the session and not SET LOCAL: measured, a
// SET LOCAL outside a transaction block is a warning and a no-op, and the autocommit
// mode has no block for a setting to be local to. The connection closes when the run
// ends, which is the reset.
func (b MigrationBudget) statement() string {
	lock, statement := b.budgets()
	return fmt.Sprintf("SET lock_timeout TO '%s'; SET statement_timeout TO '%s'",
		pgDuration(lock, "5s"), pgDuration(statement, "0"))
}

// validate refuses a budget that could not be a wait. The caller names the door it
// was refused at, because both Migrate and Backfill take one.
func (b MigrationBudget) validate() error {
	if b.LockTimeout != nil && *b.LockTimeout < 0 {
		return fmt.Errorf("lock budget %s is a wait in the past", *b.LockTimeout)
	}
	if b.StatementTimeout != nil && *b.StatementTimeout < 0 {
		return fmt.Errorf("statement budget %s is a wait in the past", *b.StatementTimeout)
	}
	return nil
}

// pgDuration renders a duration the way a PostgreSQL time setting takes it, and the
// documented default in the words the documentation uses.
func pgDuration(d time.Duration, written string) string {
	if d == 0 {
		return written
	}
	if d%time.Second == 0 {
		return fmt.Sprintf("%ds", int64(d/time.Second))
	}
	return fmt.Sprintf("%dms", d.Milliseconds())
}

// ErrContended is what a migration that could not take a lock within the runner's
// budget returns: not a failure of the migration. Nothing new was applied, the files
// already applied stay applied, and the operator may run it again. The runner does
// not wait and retry inside itself, because it holds the composition's advisory lock
// while it waits, and a queue inside that lock stops every other replica's boot.
var ErrContended = errors.New("db: migration is contended: it could not take a lock within its budget; nothing this run had not already applied was applied, and it may be run again")

// Migrate validates the selected histories, then applies pending SQL in source
// order and numeric version order, in the mode each file's own header declares:
// transactionally (expand, the default, and contract); as one statement with no
// transaction around it (autocommit, for the nontransactional statement PostgreSQL
// refuses inside a transaction block, which is why the rule table demands such a
// statement be re-runnable); or as a batched backfill, one committed transaction per
// window of its table's primary key, resumable from the last key it committed — see
// backfill.go.
//
// A file the rule table refuses, a contract half whose expansion has not applied, and
// a grammar mistake in a header are all reported before anything of that owner is
// applied. Files behind a backfill the worker owns wait for it, and the run returns
// nil, because that process is the only one that can finish the work.
func Migrate(ctx context.Context, migrateURL string, sources ...MigrationSource) error {
	return MigrateWith(ctx, migrateURL, MigrationBudget{}, sources...)
}

// MigrateWith is Migrate with the budgets a deployment named in its configuration.
// Migrate is this function with the documented defaults, which is why every caller
// that does not name a budget still gets the lock budget.
func MigrateWith(ctx context.Context, migrateURL string, budget MigrationBudget, sources ...MigrationSource) error {
	if err := budget.validate(); err != nil {
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

	run := &runner{conn: conn, budget: budget}
	if err := run.holdCompositionLock(ctx); err != nil {
		return err
	}
	// The lock is a session lock and this session ends here, so an unreleased one
	// would go when the connection goes; the release is still attempted on a
	// context that cannot be the caller's cancellation, because the statement is
	// what lets the next replica's boot start on time rather than on timeout.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = run.releaseCompositionLock(cleanup)
	}()

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

// Default table grants serve business data. Migration history belongs only to
// its owner: an application role must not be able to forge or erase progress.
// PostgreSQL executes this statement batch in one implicit transaction.
const createLedger = `
CREATE TABLE IF NOT EXISTS schema_migrations (
	owner text NOT NULL,
	version bigint NOT NULL CHECK (version > 0),
	name text NOT NULL,
	checksum text NOT NULL,
	applied_at timestamptz NOT NULL DEFAULT clock_timestamp(),
	PRIMARY KEY (owner, version)
)`

// createProgress is where a backfill that has not finished says where to start. It is
// beside the ledger and not inside it because every reader of the ledger assumes a row
// there means applied forever, and an unfinished drain must not look like one. The row
// is deleted in the transaction that writes the history row, so the two tables
// together hold one fact.
const createProgress = `
CREATE TABLE IF NOT EXISTS schema_migration_backfill (
	owner text NOT NULL,
	version bigint NOT NULL CHECK (version > 0),
	cursor text NOT NULL DEFAULT '',
	updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
	PRIMARY KEY (owner, version)
)`

// revokeFromApplicationRole is the sweep that keeps both tables the runner owns
// unwritable: a ledger an application can edit is a release that never happened, and
// a progress row it can edit is a backfill that repeats rows.
const revokeFromApplicationRole = `
DO $$
DECLARE recipient text;
BEGIN
	FOR recipient IN
		SELECT DISTINCT CASE WHEN acl.grantee = 0 THEN 'PUBLIC'
			ELSE quote_ident(pg_get_userbyid(acl.grantee)) END
		FROM pg_class c CROSS JOIN LATERAL aclexplode(c.relacl) acl
		WHERE c.oid = %s::regclass AND acl.grantee <> c.relowner
	LOOP
		EXECUTE 'REVOKE ALL ON TABLE %s FROM ' || recipient || ' CASCADE';
	END LOOP;
END $$`

// createOwnTables makes the two tables the runner owns, if they are not there.
// CREATE TABLE IF NOT EXISTS over a table that exists is a catalog lookup, which is
// why a drain may run this half and not the sweep below.
func createOwnTables(ctx context.Context, conn *sql.Conn) error {
	for _, statement := range []string{createLedger, createProgress} {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("db: migrate: create history: %w", err)
		}
	}
	return nil
}

// revokeOwnTables takes away what the application must not hold. It runs behind the
// composition's advisory lock so that one run sweeps once, and because the sweep and
// the CREATE beside it are the same statement about who owns the two tables — not
// because the REVOKE has to wait for anything: measured, it takes SHARE UPDATE
// EXCLUSIVE, which conflicts with another REVOKE or ALTER and not with the reads and
// writes of a running request or a committing batch (a plain ALTER TABLE on the same
// table behind the same reader waits; this statement completes in twenty
// milliseconds).
func revokeOwnTables(ctx context.Context, conn *sql.Conn) error {
	for _, statement := range []string{
		fmt.Sprintf(revokeFromApplicationRole, "'schema_migrations'", "schema_migrations"),
		fmt.Sprintf(revokeFromApplicationRole, "'schema_migration_backfill'", "schema_migration_backfill"),
	} {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("db: migrate: revoke history: %w", err)
		}
	}
	return nil
}

// adoptHistory re-owns the ledger rows the adoptions name, all in one
// transaction, so a failure part-way leaves the history as it was. A row the
// old owner never applied is not there to adopt, which is every fresh
// installation and every installation that already adopted it; a row that is
// there with another checksum is a file that changed after it was applied.
func adoptHistory(ctx context.Context, conn *sql.Conn, adoptions []adoption) error {
	if len(adoptions) == 0 {
		return nil
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("db: migrate: adopt history: %w", err)
	}
	defer tx.Rollback()
	for _, a := range adoptions {
		result, err := tx.ExecContext(ctx,
			"UPDATE schema_migrations SET owner = $1 WHERE owner = $2 AND version = $3 AND checksum = $4",
			a.owner, a.from, a.version, a.checksum)
		if err != nil {
			return fmt.Errorf("db: migrate: adopt %s/%s from %s: %w", a.owner, a.name, a.from, err)
		}
		if moved, _ := result.RowsAffected(); moved == 1 {
			continue
		}
		var applied string
		err = tx.QueryRowContext(ctx, "SELECT name FROM schema_migrations WHERE owner = $1 AND version = $2", a.from, a.version).Scan(&applied)
		switch {
		case err == sql.ErrNoRows:
			// Nothing to adopt: never applied under the old owner, or adopted already.
		case err != nil:
			return fmt.Errorf("db: migrate: adopt %s/%s from %s: %w", a.owner, a.name, a.from, err)
		default:
			return fmt.Errorf("db: migrate: %s/%s was applied as %s/%s with different content; adoption keeps the bytes that ran, add a new migration for the change",
				a.owner, a.name, a.from, applied)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("db: migrate: adopt history: %w", err)
	}
	return nil
}

// migrationHistory is what the ledger said before this run began, which is the
// only thing a release rule can be judged against: the contract half asks whether
// its expansion has *already* applied, and a drain asks whether anybody was
// reading this owner before this run started.
type migrationHistory struct {
	applied map[migrationID]bool
	latest  map[string]int64
}

func pendingMigrations(ctx context.Context, conn *sql.Conn, migrations []migration) ([]migration, migrationHistory, error) {
	remaining := make(map[migrationID]migration, len(migrations))
	history := migrationHistory{applied: map[migrationID]bool{}, latest: map[string]int64{}}
	for _, migration := range migrations {
		remaining[migration.migrationID] = migration
		history.latest[migration.owner] = 0
	}
	rows, err := conn.QueryContext(ctx, "SELECT owner, version, name, checksum FROM schema_migrations ORDER BY owner, version")
	if err != nil {
		return nil, history, fmt.Errorf("db: migrate: read history: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id migrationID
		var name, checksum string
		if err := rows.Scan(&id.owner, &id.version, &name, &checksum); err != nil {
			return nil, history, fmt.Errorf("db: migrate: read history: %w", err)
		}
		if _, selected := history.latest[id.owner]; !selected {
			continue
		}
		migration, found := remaining[id]
		if !found {
			return nil, history, fmt.Errorf("db: migrate: %s/%s was applied but is missing from this release", id.owner, name)
		}
		if migration.name != name || migration.checksum != checksum {
			return nil, history, fmt.Errorf("db: migrate: %s/%s changed after it was applied; add a new migration", id.owner, name)
		}
		history.applied[id] = true
		history.latest[id.owner] = max(history.latest[id.owner], id.version)
		delete(remaining, id)
	}
	if err := rows.Err(); err != nil {
		return nil, history, fmt.Errorf("db: migrate: read history: %w", err)
	}
	var pending []migration
	for _, migration := range migrations {
		if _, needed := remaining[migration.migrationID]; !needed {
			continue
		}
		if migration.version < history.latest[migration.owner] {
			return nil, history, fmt.Errorf("db: migrate: %s/%s precedes applied version %d; append a new version", migration.owner, migration.name, history.latest[migration.owner])
		}
		pending = append(pending, migration)
	}
	return pending, history, nil
}

// groupedByOwner keeps each owner's pending files together and in version order,
// which is how they arrive: readMigrations appends one source at a time, sorted.
// The grouping is what lets a release rule be judged over a whole owner before any
// of its files runs.
func groupedByOwner(pending []migration) [][]migration {
	var groups [][]migration
	for i, m := range pending {
		if i == 0 || pending[i-1].owner != m.owner {
			groups = append(groups, nil)
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], m)
	}
	return groups
}

// planOwner is the release's own order, read before a single statement runs.
//
// Two rules are about a release rather than a file, and neither can be enforced
// halfway through one. A contract half may not run in the same release as the
// expansion it removes: on an installation that already has history, the expansion has
// to be in the ledger before this run began, or nothing this owner has pending is
// applied — a run that applied the expansion and then refused the contract would have
// done half the thing the guard exists to stop.
//
// The second is about a phase=data file whose owner already has history: a drain over
// a table with readers, and what decides who runs it is what waits behind it. Files
// behind the drain cannot apply until it finishes, so this run stops at the file,
// applies nothing further for the owner, and reports what it left: the release cannot
// complete in this run either way, and the worker — which drains under readers because
// nothing waits on its boot — owns the work. Not a failure: failing here would stop the
// only role that can finish it. It is decided before the contract half is consulted, so
// a contract file behind a drain waits rather than refusing. Two drains are this run's.
// One already started is resumed, because a progress row is a version that is neither
// applied nor un-started, which is no state a later run may choose to leave. A data
// file with nothing of its owner pending behind it is drained here, because there is
// nothing left to keep in order and this run can bound the work: a table longer than
// the bound ends the run with ErrBackfillBudget and the tick takes the rest. A body
// that declared itself bounded has no window, so nothing bounds it and it stays the
// worker's even when it is last. An owner with no history at all is the opposite case:
// nobody is reading, and the rows are the ones this installation is writing, so its
// data files drain here and now, bounded.
func planOwner(ctx context.Context, conn *sql.Conn, files []migration, history migrationHistory) ([]migration, error) {
	owner := files[0].owner
	if history.latest[owner] == 0 {
		return files, nil
	}
	for i, m := range files {
		if m.phase == phaseData {
			progress := drainCursor(ctx, conn, m.migrationID)
			if progress.err != nil {
				return nil, progress.err
			}
			// The drain this run owns: one it found in flight, and the owner's last
			// pending file, which has nothing behind it and a window to bound.
			if progress.started || (i == len(files)-1 && m.windowed()) {
				continue
			}
			slog.WarnContext(ctx, "db: left migrations pending behind a backfill the worker drains",
				"owner", owner, "version", m.version, "remaining", len(files)-i)
			return files[:i], nil
		}
		if m.phase == phaseContract && !history.applied[migrationID{owner, m.contractOf}] {
			return nil, refusal(refusalMissingExpansion,
				fmt.Sprintf("%s/%s waits for %s of the same owner, which this installation has not applied yet", owner, m.name, partnerFile(files, m.contractOf)),
				"the contract half runs in the release after the expansion it removes")
		}
	}
	return files, nil
}

// partnerFile names the version a contract half waits for as a file, because that
// is what an operator greps for and what a release note lists.
func partnerFile(files []migration, version int64) string {
	for _, m := range files {
		if m.version == version {
			return m.name
		}
	}
	return fmt.Sprintf("version %d", version)
}

// compositionLockKey is the advisory lock one migration run holds for its whole
// composition, so that two replicas do not apply the same file at once.
const compositionLockKey = 7240101

// runner is one pinned connection and the budgets in force for one run. Every
// statement the runner sends on it is preceded by the budgets being re-asserted,
// which is what stops a file that sets a budget for itself from leaving it on the
// connection for the next file.
type runner struct {
	conn   *sql.Conn
	budget MigrationBudget
	locked bool // whether this session holds compositionLockKey right now
}

// holdCompositionLock takes the composition's advisory lock for this session.
//
// The wait for it is patient and unbounded on purpose, and it is not the patience the
// files are given: the budgets go on the session after the lock is held, so
// pg_advisory_lock itself waits with none of them. That is the right shape — the run
// that gets the lock second has the first one's applied files to read and finds
// nothing pending, so a replica that waits an hour and applies nothing beats one that
// refuses at five seconds and is read as a failed deploy. What bounds this wait is the
// caller's context, and when that runs out the operator gets a context deadline, not
// ErrContended: nothing was refused, the run simply did not finish.
func (r *runner) holdCompositionLock(ctx context.Context) error {
	if r.locked {
		return nil
	}
	if _, err := r.conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", compositionLockKey); err != nil {
		return fmt.Errorf("db: migrate: lock: %w", r.refused(err))
	}
	r.locked = true
	return nil
}

// releaseCompositionLock gives the lock back, and is quiet about a session that
// already gave it back.
func (r *runner) releaseCompositionLock(ctx context.Context) error {
	if !r.locked {
		return nil
	}
	if _, err := r.conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", compositionLockKey); err != nil {
		return fmt.Errorf("db: migrate: unlock: %w", err)
	}
	r.locked = false
	return nil
}

func (r *runner) apply(ctx context.Context, migration migration) (drainReport, error) {
	if err := r.budgets(ctx); err != nil {
		return drainReport{}, err
	}
	switch {
	case migration.phase == phaseData:
		return r.drain(ctx, migration, installBackfillBatches)
	case migration.autocommit:
		// The composition's lock goes down for this one statement and comes back
		// afterwards, because a nontransactional statement waits for the transactions
		// already in the database — that is what CONCURRENTLY is for, and no
		// lock_timeout bounds that wait — while the lock it would otherwise be held
		// under is the one every other replica's boot queues behind. Measured, holding
		// both at once deadlocks the queue; ADR 0011 carries the deadlock DETAIL. What
		// two replicas may then both reach is a statement the rule table already
		// demands be re-runnable, and recordRerunnableHistory turns a lost race into a
		// file that applied rather than a boot that failed.
		if err := r.releaseCompositionLock(ctx); err != nil {
			return drainReport{}, err
		}
		_, execErr := r.conn.ExecContext(ctx, migration.sql)
		if lockErr := r.holdCompositionLock(ctx); lockErr != nil {
			return drainReport{}, errors.Join(execErr, lockErr)
		}
		if execErr != nil {
			return drainReport{}, execErr
		}
		return drainReport{}, recordRerunnableHistory(ctx, r.conn, migration)
	}
	tx, err := r.conn.BeginTx(ctx, nil)
	if err != nil {
		return drainReport{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, migration.sql); err != nil {
		return drainReport{}, err
	}
	if err := recordIn(ctx, tx, migration); err != nil {
		return drainReport{}, err
	}
	return drainReport{}, tx.Commit()
}

const insertHistory = "INSERT INTO schema_migrations (owner, version, name, checksum) VALUES ($1, $2, $3, $4)"

// recordHistory commits one file's history row in a transaction of its own, which
// is what an autocommit file leaves behind: its statement already ran, and the row
// that says so is the one thing still allowed to be atomic.
func recordHistory(ctx context.Context, conn *sql.Conn, migration migration) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := recordIn(ctx, tx, migration); err != nil {
		return err
	}
	return tx.Commit()
}

// recordRerunnableHistory is recordHistory for the one file whose statement another
// session may have been running at the same moment: a conflict on the primary key is
// not this run failing, it is the other run having succeeded.
func recordRerunnableHistory(ctx context.Context, conn *sql.Conn, migration migration) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, insertHistory+` ON CONFLICT (owner, version) DO NOTHING`,
		migration.owner, migration.version, migration.name, migration.checksum); err != nil {
		return err
	}
	return tx.Commit()
}

func recordIn(ctx context.Context, tx *sql.Tx, migration migration) error {
	_, err := tx.ExecContext(ctx, insertHistory, migration.owner, migration.version, migration.name, migration.checksum)
	return err
}

// budgets puts the run's budgets on its own session, before every file and every
// batch. On the session and not SET LOCAL: measured, a SET LOCAL outside a
// transaction block is a warning and a no-op, and the autocommit mode has no block
// for a setting to be local to. Re-asserted every time, so that a file which sets a
// budget for itself cannot leak it to the next one; the connection closes when the
// run ends, which is the reset.
func (r *runner) budgets(ctx context.Context) error {
	if _, err := r.conn.ExecContext(ctx, r.budget.statement()); err != nil {
		return fmt.Errorf("db: migrate: budgets: %w", err)
	}
	return nil
}

// refused is what a lock timeout becomes: not a migration that failed, a migration
// that declined to wait.
func (r *runner) refused(err error) error {
	if !contended(err) {
		return err
	}
	lock, statement := r.budget.budgets()
	return fmt.Errorf("%w (lock_timeout %s, statement_timeout %s): %w",
		ErrContended, pgDuration(lock, "5s"), pgDuration(statement, "0"), err)
}

// contended reports the one PostgreSQL state that is not a migration failing: the
// wait for a lock ran out, which says nothing about the file and everything about
// who was holding the table.
func contended(err error) bool {
	pg, isPostgres := errors.AsType[*pgconn.PgError](err)
	return isPostgres && pg.Code == "55P03"
}
