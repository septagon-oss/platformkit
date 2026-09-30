package db_test

// composition_lock_rehold_test.go pins the second half of the README's sentence about
// the composition lock — that the wait for it "is a different wait, and it is left
// patient" — at the one moment a run reaches for that lock with its own budgets already
// on its session: the re-acquisition after an `autocommit=true` file, the only place
// `Migrate` puts the budgets on its session (kit/db/migrate.go, `apply`) before it asks
// for the lock back (`stepAwayFromTheCompositionLock`, `stepBackToTheCompositionLock`).
//
// The first acquisition is covered by review3_guard_floor_test.go's
// TestTheCompositionLockWaitsOnTheCallersContextNotOnABudget, which can only cover it
// because the budgets are not on the session yet. The re-acquisition is no abstraction:
// every test of this repository that calls `db.Migrate` contends for one advisory lock
// per database — the tests of a package get one schema each, not one database each, and
// docs/adr/0011 prints the lock tag with the database's oid in it — and the run that
// reached the re-acquisition second refused at the file's five seconds with ErrContended,
// the answer `holdCompositionLock`'s own comment rules out: a run "that refuses at five
// seconds and is read as a failed deploy". A contention the operator is told to retry,
// from the run that was told to wait.
//
// The arrangement below arranges that wait and nothing else, because the arrangement this
// file first carried cost the suite more than it cost itself: its second session took the
// key whenever it found it free, eight seconds at a time, until the run ended, and the key
// it held, held nearly the whole time, is the one every other package's boot migration
// queues behind in this database. Measured: this case's own run waited 8.9s, its holder
// won the key twice in one pass of it, and the failure that came out of `make check`
// belonged to a sibling package, whose boot lost its ten-second context in that queue. So:
//
//   - the table the file rebuilds, its seed and its indexes are made before the run is
//     called, because the work a run does *while it holds the key* is precisely what the
//     suite queues behind. The seconds the suite pays for this case are its three files,
//     not a 50000-row insert;
//   - one session holds an open snapshot on that table, so the rebuild waits and the window
//     stays open at this case's pace rather than the machine's — the same wait the sibling
//     case pins, which needs no loaded machine either;
//   - the second session reaches for the key only after it has seen the rebuild running
//     under this test's own application_name. The one moment in the run when the key is
//     legitimately free is the moment the run itself put it down: reaching for it then, and
//     waiting for it rather than spinning for it, means this case neither queues ahead of a
//     run that holds the key nor takes it out of that run's hands;
//   - the key then comes back at the earliest of three moments — the run answering while it
//     is held (which is the refusal asserted below), `reholdHold` past the moment the run is
//     seen waiting for it, or `reholdHoldCeiling`. One hold, bounded by a constant this file
//     states, inside a window in which the run itself holds no lock at all.
//
// Whether that hold overlapped the run's wait stays the diagnostic this file always logged
// it as, and for the reason it gave: a concurrent rebuild's wait for the transactions
// already in the database is the one wait no `lock_timeout` bounds (ADR 0011), so on a suite
// with several packages migrating at once the run can still be inside its statement when the
// hold ends. Requiring the sighting would make this case end with an argument about the
// machine, which is what the whole arrangement above exists to avoid; what it must never do
// is stand on the shared key for longer than it says it will.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// reholdCompositionLock is kit/db's composition key (kit/db/migrate.go:
// compositionLockKey), spelled out here rather than borrowed from another file's
// declaration so this case stands on its own.
const reholdCompositionLock = 7240101

// reholdColumns is how many indexed columns the table carries: the one REINDEX statement
// of the autocommit file rebuilds the table and every one of them, and the count after the
// run is what says the file finished. The table is small on purpose — the open snapshot
// below, not the size of the work, is what keeps the window open.
const reholdColumns = 3

// reholdLockBudget is the patience the run is configured with. Both hold lengths below are
// multiples of it, so a run that answered this wait with its budget refused by arithmetic
// rather than by the machine's speed — and the budget itself stays short, so the case
// measures that arithmetic well inside its own run.
const reholdLockBudget = time.Second

// reholdHold is how long the key is kept past the moment the run is *seen* waiting for it:
// twice the budget above, which is what makes the wait under test longer than that budget
// by a stated amount rather than incidentally. reholdHoldCeiling is the most this case will
// ever stand on the key having seen nothing — four budgets, because a run carrying the wrong
// one is refused inside one, and holding past that buys the case nothing it can still
// observe: under contention an eight-budget ceiling was paid in full for a wait never seen.
const (
	reholdHold        = 2 * reholdLockBudget
	reholdHoldCeiling = 4 * reholdLockBudget

	// reholdStatementSee is how long the case will wait to see its own rebuild start: the
	// run has to get the composition key before any of this happens, and under a `make
	// check` in which every package boots a schema at once, waiting a minute for that turn
	// is normal rather than a sign that nothing is happening.
	reholdStatementSee = 3 * time.Minute
)

// TestTheCompositionLockIsWaitedForWithoutTheFileBudgetAfterAnAutocommitFile gives the
// run a short lock budget and holds the composition lock from a second session across the
// window its autocommit file opens. What that session wants is the lock and nothing else;
// what the run does while it is without the lock is a statement that waits and a wait for
// the lock back, and neither is the file's to be refused by. A refusal therefore names
// which of the two answered with the budget, and either of them answering with it is the
// bug this case is about — autocommit_statement_waits_past_the_lock_budget_test.go pins
// the statement half on its own, against an arranged wait rather than this case's weather.
func TestTheCompositionLockIsWaitedForWithoutTheFileBudgetAfterAnAutocommitFile(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	admin := dbtest.Open(t, migrateURL)
	schema := dbtest.DeploymentSchema(t, admin)

	// The table, its indexed columns and autovacuum turned off, before the run is called:
	// the seed and the index builds belong here, where nothing waits for the composition
	// key, and not in a file of the run. Autovacuum goes off for the reason the sibling case
	// gives it — a worker started against the table is a second waiter this case did not
	// arrange and would have to time around.
	declared, listed, selected, indexes := &strings.Builder{}, &strings.Builder{}, &strings.Builder{}, &strings.Builder{}
	for i := range reholdColumns {
		column := fmt.Sprintf("c%d", i)
		fmt.Fprintf(declared, ", %s bigint", column)
		fmt.Fprintf(listed, ", %s", column)
		fmt.Fprintf(selected, ", g%%%d", 7+i)
		fmt.Fprintf(indexes, "\nCREATE INDEX probe_%s_idx ON probe (%s);", column, column)
	}
	exec(t, t.Context(), admin, "CREATE TABLE probe (id bigint PRIMARY KEY"+declared.String()+");"+
		"\nINSERT INTO probe (id"+listed.String()+") SELECT g"+selected.String()+
		" FROM generate_series(1, 2000) g;"+indexes.String()+
		"\nALTER TABLE probe SET (autovacuum_enabled = false)")

	// The transaction already in the database: the reader a concurrent rebuild exists to
	// wait for, and what keeps the window this case acts inside open until the case is
	// standing in it.
	blocker, err := admin.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	exec(t, t.Context(), blocker, "BEGIN; SELECT count(*) FROM probe")

	// The other run of this composition: one connection, which reaches for the key when and
	// only when the case tells it to, and keeps it for one of the two lengths above.
	holder := dbtest.Open(t, migrateURL)
	holderConn, err := holder.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer holderConn.Close()
	var holding bool
	t.Cleanup(func() {
		if holding {
			_, _ = holderConn.ExecContext(context.WithoutCancel(t.Context()),
				"SELECT pg_advisory_unlock($1)", reholdCompositionLock)
		}
	})

	lock := reholdLockBudget
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Minute)
	defer cancel()
	applied := make(chan error, 1)
	go func() {
		// A ledger file first, so the run reaches its autocommit file the way a real
		// composition does: with a file already applied under the key it holds.
		applied <- db.MigrateWith(ctx, migrateURL, db.MigrationBudget{LockTimeout: &lock},
			db.MigrationSource{Owner: "rehold", Files: fstest.MapFS{
				"000001_ledger.up.sql":  {Data: []byte("CREATE TABLE rehold_ledger (id bigint PRIMARY KEY)")},
				"000002_reindex.up.sql": {Data: []byte("-- pkit: autocommit=true\nREINDEX (CONCURRENTLY) TABLE probe")},
			}})
	}()

	// The window opens at the statement, so the statement is where the case waits for it:
	// until this line is in pg_stat_activity the run holds the key, and reaching for it then
	// would be this case queueing against a run that holds it rather than one that put it
	// down. It is looked for under this test's own application_name, which is what keeps the
	// question this case asks about this run and not about a sibling's.
	watch := dbtest.Open(t, migrateURL)
	if !watchForStatement(t, watch, "REINDEX (CONCURRENTLY) TABLE probe", reholdStatementSee) {
		t.Fatal("the run was never seen inside its concurrent rebuild, so this case measured nothing")
	}
	if _, err := holderConn.ExecContext(t.Context(),
		"SELECT pg_advisory_lock($1)", reholdCompositionLock); err != nil {
		t.Fatalf("take the composition key the run put down: %v", err)
	}
	holding = true
	// The rebuild may go on its way now: what this case wants is what the run does when it
	// reaches for the key and finds it held.
	exec(t, context.WithoutCancel(t.Context()), blocker, "COMMIT")

	heldFor, overlapped, refused := holdTheKey(t, watch, schema, func() error {
		_, err := holderConn.ExecContext(context.WithoutCancel(t.Context()),
			"SELECT pg_advisory_unlock($1)", reholdCompositionLock)
		return err
	}, applied)
	holding = false
	switch {
	case errors.Is(refused, db.ErrContended):
		t.Errorf("the run answered with its file's %s budget from inside the window it opens for the autocommit statement, having put the lock down for it: %v — the chain names which half of that window refused, and neither half is the file's to answer for: the budgets bound what a file waits inside a transaction, and the caller's context bounds the run's own waits", reholdLockBudget, refused)
	case refused != nil:
		t.Fatalf("the run refused: %v", refused)
	}
	if n := countRows(t, dbtest.Open(t, migrateURL), "SELECT count(*) FROM pg_indexes WHERE indexname LIKE 'probe\\_c%\\_idx' ESCAPE '\\'"+
		" AND schemaname = current_schema()"); n != reholdColumns {
		t.Errorf("%d of %d indexes after the run that reindexed them: the autocommit file did not finish", n, reholdColumns)
	}
	t.Logf("held the key %s (ceiling %s), the run's own wait inside it: %v — a run still inside its rebuild when the hold ends answers the question this case asks all the same, and this says whether it was waited for",
		heldFor.Round(time.Millisecond), reholdHoldCeiling, overlapped)
}

// holdTheKey keeps the composition key from the moment it is handed to it, gives it back
// through release at the earliest of the three moments the comment above names, and answers
// with the run's own error: how long the key was held, whether the run was seen waiting for
// it in that span, and what the run finally returned. Everything it waits on is either a
// constant above or the run itself, so the hold cannot stretch because of a machine and
// cannot end before the budget it exists to outlast has been passed twice over.
func holdTheKey(t *testing.T, watch *sql.DB, applicationName string, release func() error, applied <-chan error) (time.Duration, bool, error) {
	t.Helper()
	started := time.Now()
	var sighted time.Time
	var early error
	for {
		select {
		case refused := <-applied:
			early = refused
		default:
		}
		if early != nil {
			break
		}
		if !sighted.IsZero() && time.Since(sighted) >= reholdHold {
			break
		}
		if time.Since(started) >= reholdHoldCeiling {
			break
		}
		if sighted.IsZero() && runWaitsForThisKey(t, watch, applicationName) {
			sighted = time.Now()
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := release(); err != nil {
		t.Fatalf("give the composition key back: %v", err)
	}
	if early != nil {
		return time.Since(started), !sighted.IsZero(), early
	}
	held := time.Since(started)
	err := <-applied
	return held, !sighted.IsZero(), err
}

// runWaitsForThisKey says whether a session of this test — named by the application_name
// every URL dbtest returns carries, which is why the question goes to this test's own schema
// rather than to anything waiting on an advisory lock anywhere in the database — is right now
// waiting on one. `pg_stat_activity` is the only account of what a session is doing at this
// instant, and it names this wait `advisory` under the `Lock` type: the run inside
// `pg_advisory_lock`, seen rather than inferred from a stopwatch, which is why the hold above
// can end on sight instead of at the end of a sleep somebody had to guess the length of.
func runWaitsForThisKey(t *testing.T, watch *sql.DB, applicationName string) bool {
	t.Helper()
	var waiting int
	scan(t, watch, "SELECT count(*) FROM pg_stat_activity WHERE datname = current_database()"+
		" AND pid <> pg_backend_pid() AND wait_event_type = 'Lock' AND wait_event = 'advisory'"+
		" AND application_name = "+quoteLiteral(applicationName), &waiting)
	return waiting > 0
}
