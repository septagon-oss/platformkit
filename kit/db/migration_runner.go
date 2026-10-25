package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// runner is one pinned connection and the budgets in force for one run. Every file and
// every batch of the run is sent with the budgets re-asserted on the session first,
// which is what stops a file that sets a budget for itself from leaving it on the
// connection for the next file. The budgets are the run's patience for what happens
// *inside* a transaction; the one window that runs without them is the one the run goes
// without the composition lock in: see stepAwayFromTheCompositionLock.
type runner struct {
	conn   *sql.Conn
	budget MigrationBudget
	locked bool  // whether this session holds the composition lock right now
	schema int64 // the namespace the lock was taken under, read once: see compositionSchema
	// lockedFile is the "owner/version" of the file this session is running outside a
	// transaction, whose lock it therefore holds; empty when it holds none.
	lockedFile string
}

// holdCompositionLock serializes migration runs in the current schema's namespace.
// The caller's context bounds the wait; file budgets are disabled while waiting.
// A missing namespace is refused. Deadlock victims retry through execReaskable
// before any file runs. See README.md for the lock and budget lifecycle.
func (r *runner) holdCompositionLock(ctx context.Context) error {
	if r.locked {
		return nil
	}
	schema, err := r.compositionSchema(ctx)
	if err != nil {
		return err
	}
	if err := r.execReaskable(ctx,
		"SELECT pg_advisory_lock($1::int, $2::oid::int)", compositionLockKey, schema); err != nil {
		return fmt.Errorf("db: migrate: lock: %w", r.refused(err))
	}
	r.locked = true
	return r.budgets(ctx)
}

// reasksAfterDeadlock is how many times a statement is re-issued after PostgreSQL picks
// this session out of a deadlock, for the statements a run is allowed to send twice: the
// ask for the composition lock, and the drop half of a concurrent index repair
// (kit/db/certify.go), which is `DROP INDEX CONCURRENTLY IF EXISTS`. It bounds the retry
// rather than aiming at a schedule: the cancellation breaks one cycle, and a database whose
// next wait joins another is a state a boot should name rather than sit in — with no wait
// between the re-asks, this many are all the retry costs. The other half of that repair, the
// rebuild, is not one of these statements and does not go through this one: a cancelled
// concurrent build leaves its invalid index behind, so its retry is a whole cycle rather
// than a re-send, bounded beside this constant by repairAttempts.
const reasksAfterDeadlock = 4

// execReaskable sends one of the statements named above and re-sends it while the server
// keeps choosing this session out of somebody else's cycle, or while it answers that the
// catalog row this statement went to change was being changed by someone else. A cancelled
// one of these left nothing behind and undid nothing: an ask holds nothing but the lock it
// is asking for, the drop is written to answer the same way twice, and the only migration
// statement re-sent here is one the rule table refuses unless it is re-runnable. Anything
// else the server answers — a budget that ran out, a statement that is simply wrong — comes
// back to the caller unchanged, on the first answer it gives.
func (r *runner) execReaskable(ctx context.Context, statement string, args ...any) error {
	for attempt := 0; ; attempt++ {
		_, err := r.conn.ExecContext(ctx, statement, args...)
		if err == nil || !(deadlockVictim(err) || catalogTupleRace(err)) || attempt >= reasksAfterDeadlock {
			return err
		}
	}
}

// deadlockVictim reports the answer that says "this session was chosen to step out of a
// wait" rather than "this run's work is wrong": 40P01, deadlock_detected, with nothing of
// the cancelled statement kept. It is read apart from contended, which reports a budget
// that ran out — that one is the deployment's own limit, stated as ErrContended, and it
// stays the refusal it is.
func deadlockVictim(err error) bool {
	pg, isPostgres := errors.AsType[*pgconn.PgError](err)
	return isPostgres && pg.Code == "40P01"
}

// catalogTupleRace reports the answer that says "someone else was changing the catalog row
// this statement came for, and still is": XX000 with the one message that means it. It is
// what two sessions dropping and rebuilding one index do to each other's pg_class tuple,
// and, unlike a build the detector cancelled, it leaves nothing half-built behind — the
// statement simply did not happen, which is why it is one of the statements execReaskable
// re-sends. Read apart from a plain XX000, which is the server saying it has no idea what
// went wrong: re-sending that would be a guess at a diagnosis.
func catalogTupleRace(err error) bool {
	pg, isPostgres := errors.AsType[*pgconn.PgError](err)
	return isPostgres && pg.Code == "XX000" && strings.Contains(pg.Message, "tuple concurrently updated")
}

// holdFileLock serializes one autocommit file while its composition lock is down.
// It polls a try-lock, leaving the session idle between attempts so a concurrent
// index build cannot wait on the lock waiter's snapshot. The caller's context
// bounds the wait; releaseFileLock gives the session lock back.
func (r *runner) holdFileLock(ctx context.Context, file string) error {
	if r.lockedFile != "" {
		return nil
	}
	for {
		var taken bool
		if err := r.conn.QueryRowContext(ctx, fileLockTry, fileLockSpace, file).Scan(&taken); err != nil {
			return fmt.Errorf("db: migrate: the file lock on %s: %w", file, err)
		}
		if taken {
			r.lockedFile = file
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("db: migrate: the file lock on %s is held by another boot running the same file: %w", file, ctx.Err())
		case <-time.After(fileLockWait):
		}
	}
}

// fileLockTry asks for the file lock without queueing for it: see holdFileLock.
const fileLockTry = "SELECT pg_try_advisory_lock($1, hashtext($2))"

// fileLockWait is how long a boot leaves its session idle between asks for a file another
// boot is running. Long enough that no build has to be polled for, short enough that a boot
// that came to the wrong moment does not notice the wait.
const fileLockWait = 50 * time.Millisecond

// releaseFileLock gives the file this run is running its lock back. An unlock waits for
// nothing, so it goes out with the run's budgets on the session.
func (r *runner) releaseFileLock(ctx context.Context) error {
	if r.lockedFile == "" {
		return nil
	}
	if _, err := r.conn.ExecContext(ctx, fileLockRelease, fileLockSpace, r.lockedFile); err != nil {
		return fmt.Errorf("db: migrate: release the file lock on %s: %w", r.lockedFile, err)
	}
	r.lockedFile = ""
	return nil
}

// fileLockSpace namespaces the advisory lock an autocommit file takes for itself, and the
// two statements take and give it back. The two-key form is deliberate: it puts this
// space in pg_locks.classid, where the composition lock's row carries its own
// (compositionLockKey, with this namespace's OID as objid — see holdCompositionLock), so
// a reader tells the two locks apart by classid alone.
// The repair tests read the composition lock by that classid to
// show a repair that holds it, and a lock the repair is entitled to hold must not read as
// the fact that case injects around.
//
// hashtext is the server's own hash of the file's name — the same choice db.TryLock makes
// (kit/db/lock.go), for its reason: two replicas that spell the file the same way take the
// same lock, with no registry of numbers to keep in step. Two files whose names collide in
// one 32-bit hash cost each other a wait for work neither is doing, and nothing else.
const fileLockSpace = 7240102

const (
	fileLockRelease = "SELECT pg_advisory_unlock($1, hashtext($2))"
)

// compositionSchema is the namespace the run's own session resolves to, as the OID the
// lock's second half takes. An OID is unsigned 32-bit, so a namespace created after the
// cluster's counter passed 2^31 has an OID no int holds: the value is carried as a bigint
// and the two lock statements wrap it with `::oid::int`, which is how Postgres itself
// narrows an OID into an int and is the key the suite's own lock lines contend. Read once
// and kept on the runner: the lock has to be given back under the key it was taken under,
// whatever the session's path holds by the time it is given back — and a file may set one.
//
// The OID comes from pg_namespace by name rather than from to_regnamespace(current_schema()):
// that function parses its argument the way a statement parses an identifier, while
// current_schema() answers an already-resolved name, so a legal name the grammar cannot hold
// unquoted — one with a space or a capital — would name no namespace there and the run would
// refuse a deployment whose schema exists.
func (r *runner) compositionSchema(ctx context.Context) (int64, error) {
	if r.schema != 0 {
		return r.schema, nil
	}
	var schema int64
	if err := r.conn.QueryRowContext(ctx,
		"SELECT coalesce((SELECT oid FROM pg_namespace WHERE nspname = current_schema()), 0)::oid::bigint").Scan(&schema); err != nil {
		return 0, fmt.Errorf("db: migrate: reading the composition's namespace: %w", err)
	}
	if schema == 0 {
		return 0, fmt.Errorf("db: migrate: this session resolves to no schema — current_schema() is NULL — so its history table has nowhere to land and its composition lock has no namespace to name")
	}
	r.schema = schema
	return schema, nil
}

// releaseCompositionLock gives the lock back, and is quiet about a session that
// already gave it back.
func (r *runner) releaseCompositionLock(ctx context.Context) error {
	if !r.locked {
		return nil
	}
	if _, err := r.conn.ExecContext(ctx,
		"SELECT pg_advisory_unlock($1::int, $2::oid::int)", compositionLockKey, r.schema); err != nil {
		return fmt.Errorf("db: migrate: unlock: %w", err)
	}
	r.locked = false
	return nil
}

// unbudgeted is both budgets off, spelled the way PostgreSQL spells "no limit", and is
// what the runner puts on its own session for the one wait it does not mean to refuse at
// all: the queue for the composition lock. Zero — rather than leaving the setting alone —
// because the run's session is where the file budgets already sit: `apply` puts them there
// before choosing how to run the file, and `SET LOCAL` is a warning outside a transaction
// block, which is the one block this window has none of. Both settings and not one,
// because a statement budget bounds a wait for a lock as surely as it bounds the work done
// inside the lock, and by the time the lock is reached a second time mid-run the session
// still carries the statement budget the autocommit statement was sent under.
const unbudgeted = "SET lock_timeout TO '0'; SET statement_timeout TO '0'"

// noLockBudget removes the lock timeout for an autocommit statement's snapshot
// waits while retaining the deployment's statement timeout.
const noLockBudget = "SET lock_timeout TO '0'"

// stepBackToTheCompositionLock closes that window: the lock comes back, patiently and
// bounded by the caller's context as holdCompositionLock says and as
// the floor tests pin for the first acquisition, and the budgets go back on
// before the file's history row, so the transaction that records the file is a normal
// transaction with the run's patience on it.
//
// Both budgets come off for the reach and not the lock's alone, because the statement
// above left the session still carrying the run's statement budget, and that one bounds a
// wait for a lock as surely as it bounds the work inside one. Measured on a machine
// running several suites at once, with it left on for this wait, the second run to reach
// this point answered itself with ErrContended at five seconds: a wait the run was told to
// be patient about, reported to the operator as a contention to retry, from a run that had
// the first one's applied files to read and would have found nothing pending.
func (r *runner) stepBackToTheCompositionLock(ctx context.Context) error {
	if err := r.setBudgets(ctx, unbudgeted); err != nil {
		return err
	}
	if err := r.holdCompositionLock(ctx); err != nil {
		return err
	}
	return r.budgets(ctx)
}

func (r *runner) apply(ctx context.Context, migration migration) (drainReport, error) {
	if err := r.budgets(ctx); err != nil {
		return drainReport{}, err
	}
	switch {
	case migration.phase == phaseData:
		return r.drain(ctx, migration, installBackfillBatches)
	case migration.autocommit:
		// Release the composition lock while the nontransactional statement waits.
		// The file lock serializes repair; sendRerunnable verifies the resulting object
		// before its rerunnable history is recorded. See README.md.
		if err := r.stepAwayFromTheCompositionLock(ctx); err != nil {
			return drainReport{}, err
		}
		// One file, one boot at a time: see holdFileLock. The lock goes back before the
		// composition lock is asked for again, so that no session ever waits on the
		// composition lock while holding the lock another boot's build is waiting for.
		file := fmt.Sprintf("%s/%d", migration.owner, migration.version)
		if err := r.holdFileLock(ctx, file); err != nil {
			return drainReport{}, err
		}
		applyErr := r.sendRerunnable(ctx, migration)
		// The release takes a context that cannot be the caller's cancellation for the
		// reason the composition lock's release takes one: the statement is what lets the
		// next replica's boot start on time rather than on timeout.
		released, cancelRelease := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		gateErr := r.releaseFileLock(released)
		cancelRelease()
		if gateErr != nil {
			return drainReport{}, errors.Join(applyErr, gateErr)
		}
		if lockErr := r.stepBackToTheCompositionLock(ctx); lockErr != nil {
			return drainReport{}, errors.Join(applyErr, lockErr)
		}
		if applyErr != nil {
			return drainReport{}, applyErr
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

// budgets puts the run's budgets on its own session, before every file and every
// batch. On the session and not SET LOCAL: measured, a SET LOCAL outside a
// transaction block is a warning and a no-op, and the autocommit mode has no block
// for a setting to be local to. Re-asserted every time, so that a file which sets a
// budget for itself cannot leak it to the next one; the connection closes when the
// run ends, which is the reset.
func (r *runner) budgets(ctx context.Context) error {
	return r.setBudgets(ctx, r.budget.statement())
}

// setBudgets is the one round trip both budget settings cost, whatever is being put on
// the session: the run's own pair, or the pair that takes them off.
func (r *runner) setBudgets(ctx context.Context, settings string) error {
	if _, err := r.conn.ExecContext(ctx, settings); err != nil {
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
