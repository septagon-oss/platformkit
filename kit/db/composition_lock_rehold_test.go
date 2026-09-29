package db_test

// composition_lock_rehold_test.go pins the second half of the README's sentence about
// the composition lock — that the wait for it "is a different wait, and it is left
// patient" — at the one moment a run reaches for that lock with its own budgets
// already on its session: the re-acquisition after an `autocommit=true` file, the only
// place `Migrate` puts the budgets on the session (kit/db/migrate.go, `apply`) before
// it asks for the lock back (`releaseCompositionLock` / `holdCompositionLock`).
//
// The first acquisition is covered by review3_guard_floor_test.go's
// TestTheCompositionLockWaitsOnTheCallersContextNotOnABudget, which can only cover it
// because the budgets are not on the session yet. The re-acquisition is no
// abstraction: every suite that calls `db.Migrate` against one Postgres *database*
// contends for one advisory lock — the tests of a package get one schema each, not one
// database each — and the run that reached the re-acquisition second refused at the
// file's five seconds with ErrContended, the answer `holdCompositionLock`'s own comment
// rules out: a run "that refuses at five seconds and is read as a failed deploy". A
// contention the operator is told to retry, from the run that was told to wait.
//
// This case arranges the same wait without the machine's help: a second session takes
// the composition lock in the window the autocommit file opens — the moment the run
// puts the lock down because its statement cannot run inside a transaction — and holds
// it longer than the run's own lock budget. The run that gets the lock second has the
// first one's applied files to read and finds nothing pending, so waiting is the right
// answer and the refusal is the bug.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
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

// reholdColumns is how many indexed columns the table carries: the one REINDEX
// statement of the autocommit file rebuilds the table and every one of them, which is
// what makes the window this case reaches the lock through wide enough to be reached.
const reholdColumns = 12

// reholdHold is how long the second session keeps the lock. It has to outlast the run's
// own lock budget by a wide margin — the case is about a wait the budget must not
// answer — and it stays finite so a second session that loses its run can never hold one
// test's database hostage.
const reholdHold = 8 * time.Second

// reholdLockBudget is the patience the run is configured with. It is long enough that a
// REINDEX of a table nobody else is reading finishes inside it, and short enough that a
// wait which should not be bounded by it at all — the run's own, inside the window the
// autocommit file opens — is measured against it well inside this case's own run.
const reholdLockBudget = 2 * time.Second

// TestTheCompositionLockIsWaitedForWithoutTheFileBudgetAfterAnAutocommitFile gives the
// run a short lock budget and holds the composition lock from a second session for eight
// seconds, across the window the run's autocommit file opens. What that session wants is
// the lock and nothing else; what the run does while it is without the lock is a
// statement that waits and a wait for the lock back, and neither is the file's to be
// refused by. A refusal therefore names which of the two answered with the budget, and
// either of them answering with it is the bug this case is about —
// autocommit_statement_waits_past_the_lock_budget_test.go pins the statement half on its
// own, against an arranged wait rather than this case's weather.
func TestTheCompositionLockIsWaitedForWithoutTheFileBudgetAfterAnAutocommitFile(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)

	declared, listed := &strings.Builder{}, &strings.Builder{}
	for i := range reholdColumns {
		column := fmt.Sprintf("c%d", i)
		fmt.Fprintf(declared, ", %s bigint", column)
		fmt.Fprintf(listed, ", %s", column)
	}
	indexes := &strings.Builder{}
	for i := range reholdColumns {
		column := fmt.Sprintf("c%d", i)
		fmt.Fprintf(indexes, "\nCREATE INDEX probe_%s_idx ON probe (%s);", column, column)
	}

	// The seed file turns the table's autovacuum off. Fifty thousand inserts cross the
	// launcher's threshold, and a worker started against the table holds ShareUpdateExclusive
	// across the reindex's own lock upgrade — measured: the file's statement was the one
	// cancelled at the run's budget, which says nothing about the composition lock at all.
	//
	// The second session is the other run of this composition. It takes the lock
	// whenever it finds it free and keeps it for six seconds — longer than the budget
	// below, so any wait for the lock is one the budget is not allowed to answer — and
	// it keeps trying until the run ends, because it cannot know which moment the run
	// puts the lock down.
	holder := dbtest.Open(t, migrateURL)
	holderConn, err := holder.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer holderConn.Close()

	stopped := make(chan struct{})
	var stop sync.Once
	halt := func() { stop.Do(func() { close(stopped) }) }
	defer halt()

	var wins, winsInsideTheRun atomic.Int64
	var insideRun atomic.Bool
	go func() {
		for {
			select {
			case <-stopped:
				return
			default:
			}
			var got bool
			if err := holderConn.QueryRowContext(t.Context(),
				"SELECT pg_try_advisory_lock($1)", reholdCompositionLock).Scan(&got); err != nil {
				return
			}
			if !got {
				time.Sleep(time.Millisecond)
				continue
			}
			wins.Add(1)
			if insideRun.Load() {
				winsInsideTheRun.Add(1)
			}
			hold := time.NewTimer(reholdHold)
			select {
			case <-stopped:
				hold.Stop()
			case <-hold.C:
			}
			if _, err := holderConn.ExecContext(context.WithoutCancel(t.Context()),
				"SELECT pg_advisory_unlock($1)", reholdCompositionLock); err != nil {
				return
			}
		}
	}()

	lock := reholdLockBudget
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 90*time.Second)
	defer cancel()
	started := time.Now()
	// The run goes off on its own goroutine so the second session starts after the run
	// was called rather than before it, and holds the lock from the other side of the
	// autocommit file's window.
	errc := make(chan error, 1)
	go func() {
		insideRun.Store(true)
		defer insideRun.Store(false)
		errc <- db.MigrateWith(ctx, migrateURL, db.MigrationBudget{LockTimeout: &lock},
			db.MigrationSource{Owner: "rehold", Files: fstest.MapFS{
				"000001_probe.up.sql": {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY" + declared.String() +
					");\nINSERT INTO probe (id" + listed.String() + ") SELECT g" + residues() +
					" FROM generate_series(1, 50000) g;\nALTER TABLE probe SET (autovacuum_enabled = false);" +
					indexes.String())},
				"000002_reindex.up.sql": {Data: []byte("-- pkit: autocommit=true\nREINDEX (CONCURRENTLY) TABLE probe")},
			}})
	}()
	err = <-errc
	halt()
	waited := time.Since(started)

	switch {
	case errors.Is(err, db.ErrContended):
		t.Errorf("the run answered with its file's %s budget from inside the window it opens for the autocommit statement, having put the lock down for it: %v — the chain names which half of that window refused, and neither half is the file's to answer for: the budgets bound what a file waits inside a transaction, and the caller's context bounds the run's own waits", reholdLockBudget, err)
	case err != nil:
		t.Fatalf("the run refused: %v", err)
	}
	if n := countRows(t, dbtest.Open(t, migrateURL), "SELECT count(*) FROM pg_indexes WHERE indexname LIKE 'probe\\_c%\\_idx' ESCAPE '\\'"+
		" AND schemaname = current_schema()"); n != reholdColumns {
		t.Errorf("%d of %d indexes after the run that reindexed them: the autocommit file did not finish", n, reholdColumns)
	}
	// A diagnostic, not an assertion: a run that was never interrupted answers the
	// question this case asks all the same, and this says whether it was.
	t.Logf("waited %s; the second session held the lock %d times, %d of them while the run was going",
		waited.Round(time.Millisecond), wins.Load(), winsInsideTheRun.Load())
}

// residues is the SELECT side of the seed rows, one expression per column of the
// column list built above: a residue of the series value each, none of them a division
// by zero.
func residues() string {
	out := &strings.Builder{}
	for i := range reholdColumns {
		fmt.Fprintf(out, ", g%%%d", 7+i)
	}
	return out.String()
}
