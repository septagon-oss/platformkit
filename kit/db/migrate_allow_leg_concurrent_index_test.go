package db_test

import (
	"context"
	"fmt"
	"testing"
	"testing/fstest"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// TestTheRuleTablesAllowLegAppliesItsConcurrentIndexWhileTheDatabaseHasAReader is
// run 189's own file, applied. The static-rule case beside it
// (TestStaticRulesRefuseAndAllowMarks) refuses
//
//	CREATE INDEX CONCURRENTLY probe_b ON probe (b)
//
// for entering the migration's transaction, and its exception leg — the reviewer's
// answer, `-- pkit: autocommit=true` — has to actually apply that statement. In run
// 189 the refusal worked and the exception did not:
//
//	allow= did not except the rule:
//	-- pkit: autocommit=true
//	CREATE INDEX CONCURRENTLY IF NOT EXISTS probe_b ON probe (b)
//	db: migrate: probe/000002_rule.up.sql: db: migration is contended: … (lock_timeout 5s,
//	statement_timeout 0): ERROR: canceling statement due to lock timeout (SQLSTATE 55P03)
//
// The runner had put the file's own five-second budget on its session and left it
// there for the statement, so the one statement whose purpose is to wait for the
// transactions already in the database was cancelled for waiting. That is the defect
// "the whole window an autocommit file opens is outside its budgets" (101cc39, on
// main before this branch's base) cured, and
// TestAnAutocommitStatementWaitsForTheTransactionsInTheDatabasePastItsOwnLockBudget
// pins the product's side of it with a REINDEX.
//
// What this case adds is the leg that was red: the exception marker accepted, the file
// applied, and the wait supplied by the thing a concurrent build exists to wait for —
// one session holding nothing but an open snapshot on the table. Read 189 is caught
// here by the statement's own error, and the case is what puts that error within
// reach: the run is given a five-hundred-millisecond lock budget, the reader is held
// until the server itself reports the build still waiting for this session with its
// own statement four times older than that budget, and only then is the reader
// committed. Measured on the mutant where the autocommit window keeps its budgets on
// the session (kit/db/concurrent_index_case_catches_a_budget_left_on_test.py applies
// that mutation and asks for a red), the leg fails with `canceling statement due to
// lock timeout (SQLSTATE 55P03)`; over these bytes it applies, with nothing left
// invalid. It did not used to catch it: the case committed the reader the moment the
// build reported waiting, which is inside any budget, so a session still carrying its
// lock_timeout had nothing to be cancelled for and the case passed either way.
//
// The wait itself is read out of the server instead of measured around the run. It
// used to be: hold the reader in `pg_sleep(8)`, and refuse a migration that finished in
// under five seconds. That second shape fails on the schedule of the test process, not
// on the product — measured on 2026-10-04, the same correct migration applied in 1.953s
// and 2.021s twice, once with the case's own process held at a breakpoint for six of
// the reader's eight seconds (kit/db/concurrent_index_reader_schedule_test.py), which is
// a deschedule, not a defect, and is exactly the class this task exists to remove. Both
// halves are held by the case rather than by a duration: the reader is a transaction
// with an open snapshot that runs no statement and so holds until this case commits it,
// and both facts asserted are the server's own — the phase and the session it is
// waiting for in `pg_stat_progress_create_index`, and the age of the build's statement
// in `pg_stat_activity.query_start`, which is the server's clock and not this process's.
// The case therefore neither waits eight seconds nor assumes it was waited for, and the
// one duration it names is a budget the run was handed, not a threshold a run has to
// beat. The duration the leg took is still reported, in the failure message and in the
// case's own log line.
func TestTheRuleTablesAllowLegAppliesItsConcurrentIndexWhileTheDatabaseHasAReader(t *testing.T) {
	migrateURL := migrateURL(t)
	creates := &fstest.MapFile{Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, a text, b text); CREATE INDEX probe_a ON probe (a)")}
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "probe", Files: fstest.MapFS{
		"000001_probe.up.sql": creates,
	}}); err != nil {
		t.Fatalf("the file that creates the table: %v", err)
	}

	// The reader. An open transaction that has looked at the table is a snapshot the
	// rebuild may not rebuild underneath, and it holds no lock the build could take
	// alongside — which is why no lock_timeout bounds this wait and never should.
	// REPEATABLE READ is what makes the hold the transaction's own: a read-committed
	// session gives up its snapshot at the end of each statement, so a session that has
	// gone quiet in that mode blocks nothing, while in this one the snapshot lives as
	// long as the transaction does. Measured on this cluster (PostgreSQL 16, 300k rows):
	// the quiet read-committed session leaves pg_stat_progress_create_index empty and
	// the build completes in 1-2s on its own; the same session in repeatable read has
	// the build sit in phase `waiting for old snapshots` naming it, for as long as it is
	// left open. Nothing runs on the session while it holds, so no statement of this
	// case has a duration to be measured against.
	holder := dbtest.Open(t, migrateURL)
	conn, err := holder.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for _, statement := range []string{
		"BEGIN ISOLATION LEVEL REPEATABLE READ",
		"SELECT count(*) FROM probe",
	} {
		if _, err := conn.ExecContext(t.Context(), statement); err != nil {
			t.Fatalf("the reader's %s: %v", statement, err)
		}
	}
	var readerPID int64
	if err := conn.QueryRowContext(t.Context(), "SELECT pg_backend_pid()").Scan(&readerPID); err != nil {
		t.Fatal(err)
	}

	// The reader is seen, not assumed: a session this case believes is holding a
	// snapshot has to be one the server says is inside a transaction.
	admin := dbtest.Open(t, migrateURL)
	var holding bool
	scan(t, admin, fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM pg_stat_activity
		WHERE pid = %d AND state = 'idle in transaction')`, readerPID), &holding)
	if !holding {
		t.Fatal("the reader never reached the transaction it holds its snapshot with")
	}

	// The run's own lock budget, short on purpose and named here so the hold below can
	// be measured against it. This is the number a `lock_timeout` left on the session
	// would cancel the statement at; what the case has to show is the build living past
	// it. Five minutes on the context, because the wait this run may be queued on is the
	// composition lock, which every other package's boot migration is asking for too, and
	// that wait is bounded by the caller and by nothing else.
	lock := 500 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Minute)
	defer cancel()
	started := time.Now()
	finish := make(chan error, 1)
	go func() {
		finish <- db.MigrateWith(ctx, migrateURL, db.MigrationBudget{LockTimeout: &lock},
			db.MigrationSource{Owner: "probe", Files: fstest.MapFS{
				"000001_probe.up.sql": creates,
				"000002_rule.up.sql": {Data: []byte(
					"-- pkit: autocommit=true\nCREATE INDEX CONCURRENTLY IF NOT EXISTS probe_b ON probe (b)")},
			}})
	}()

	// The wait, from the server's own account: the build reports the phase it is in and
	// the session it is waiting for, and it stays in that phase until the snapshot goes,
	// which only this case can do. A run that reached the statement and never waited is
	// reported by the loop ending without the fact and the migration returning, below.
	//
	// And the hold runs past the budget the file would have carried, because that is the
	// regression's whole mechanism: a session still carrying `lock_timeout` is cancelled
	// at its budget, so a case that releases its reader inside the budget can only ever
	// pass. Both ages are the server's own — `pg_stat_activity.query_start` is when that
	// backend started this statement and `now()` is the server's clock — so the length of
	// the wait under test does not come from this process's schedule. Four times the
	// budget, the margin the REINDEX case beside it uses: it is what keeps a correct run
	// finishing from racing the cancellation the mutant produces.
	var (
		waited, heldPastBudget bool
		runErr                 error
	)
	poll := time.NewTicker(5 * time.Millisecond)
	defer poll.Stop()
	for !(waited && heldPastBudget) && runErr == nil {
		var waiting bool
		scan(t, admin, fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM pg_stat_progress_create_index
			WHERE datname = current_database() AND relid = 'probe'::regclass
			  AND phase = 'waiting for old snapshots' AND current_locker_pid = %d)`, readerPID), &waiting)
		if waiting {
			waited = true
			if !heldPastBudget {
				scan(t, admin, fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM pg_stat_activity a
					JOIN pg_stat_progress_create_index p ON p.pid = a.pid
					WHERE a.datname = current_database() AND p.relid = 'probe'::regclass
					  AND p.phase = 'waiting for old snapshots' AND p.current_locker_pid = %d
					  AND now() - a.query_start > interval '%d milliseconds')`,
					readerPID, int64((4*lock)/time.Millisecond)), &heldPastBudget)
			}
		}
		select {
		case runErr = <-finish:
		case <-poll.C:
		case <-ctx.Done():
			t.Fatal("the build never reported waiting past its own lock budget for the reader, and the case's deadline passed first")
		}
	}

	// The hold is this case's to end, and it ends here: the build may finish only from
	// this statement, so the wait it did was the wait this case gave it.
	if _, err := conn.ExecContext(t.Context(), "COMMIT"); err != nil {
		t.Fatalf("releasing the reader the build waits for: %v", err)
	}
	if runErr == nil {
		runErr = <-finish
	}
	if runErr != nil {
		t.Fatalf("the exception leg did not except the rule after %s: %v", time.Since(started).Truncate(time.Millisecond), runErr)
	}
	if !waited {
		t.Error("the allow leg applied without the build ever naming this reader as the session it waits for: the rebuild waited for nothing, which is the other way this case can be wrong")
	}
	if !heldPastBudget && runErr == nil {
		t.Errorf("the allow leg applied without the build being held past the %s lock budget it was given: the leg never had to survive the wait a leftover `lock_timeout` cancels at, so this case measured nothing", lock)
	}

	// And the index it built is a real one: a concurrent build cancelled halfway is
	// the failure that leaves an invalid index behind for a rerun to rebuild over.
	var built, invalid int
	scan(t, admin, `SELECT count(*) FROM pg_class c JOIN pg_index x ON x.indexrelid = c.oid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relname = 'probe_b' AND n.nspname = current_schema() AND x.indisvalid`, &built)
	scan(t, admin, `SELECT count(*) FROM pg_class c JOIN pg_index x ON x.indexrelid = c.oid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relname = 'probe_b' AND n.nspname = current_schema() AND NOT x.indisvalid`, &invalid)
	if built != 1 {
		t.Errorf("the allow leg left %d valid probe_b indexes, want the one it reported applying", built)
	}
	if invalid != 0 {
		t.Errorf("the allow leg left probe_b invalid, which is what a build cancelled mid-flight leaves")
	}
	t.Logf("the allow leg applied in %s with a %s lock budget; while it did, the build was held waiting for session %d for four times that budget, on the server's own account",
		time.Since(started).Truncate(time.Millisecond), lock, readerPID)
}
