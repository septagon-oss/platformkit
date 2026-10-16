package db_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"testing/fstest"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// TestAnAutocommitStatementWaitsForTheTransactionsInTheDatabasePastItsOwnLockBudget
// pins the half of an `autocommit` file's window that runs *during* its statement, not
// after it. ADR 0011 states the contract and `apply`'s comment has always repeated it:
// "a nontransactional statement waits for the transactions already in the database —
// that is what CONCURRENTLY is for, and no lock_timeout bounds that wait". The runner
// did the opposite. It put the run's budgets on its session, put the composition lock
// down for the file, and sent the statement with those budgets still in force — so the
// one statement whose whole purpose is to wait for whatever is already in the database
// was refused for waiting, cancelled in the middle of the work. For a concurrent rebuild
// that is not even a clean refusal: the new copy of each index it had already built stays
// behind invalid, and the rerun the rule requires rebuilds over the wreckage blind.
//
// The arrangement is the one the ADR names and it needs no loaded machine: one session
// that does nothing but keep an open snapshot on the table, which is a reader the rebuild
// must not rebuild underneath rather than a lock it could not take alongside. That
// snapshot is held until the run is *seen* in `pg_stat_activity` with the rebuild in hand,
// and then for four times the run's own budget over and above that, so what this case
// measures is the wait under test rather than a race with a machine's speed. Measured, with
// the budgets left on the session and a five-hundred-millisecond lock budget: `ERROR:
// canceling statement due to lock timeout`, and indexes left behind `indisvalid = false`.
// With the budgets taken off for that window: applied, every index valid, the hold's few
// seconds spent waiting.
func TestAnAutocommitStatementWaitsForTheTransactionsInTheDatabasePastItsOwnLockBudget(t *testing.T) {
	migrateURL := migrateURL(t)

	// The table and its indexes are there before the run is, because the session that
	// keeps the rebuild waiting has to hold its snapshot before the rebuild starts, and
	// it cannot hold one on a table a migration has not made yet. Autovacuum is turned
	// off for the reason the rehold case's seed gives it: a worker started against the
	// table is a second waiter this case did not arrange and would have to time around.
	admin := dbtest.Open(t, migrateURL)
	exec(t, t.Context(), admin, `CREATE TABLE probe (id bigint PRIMARY KEY, b bigint NOT NULL);
		INSERT INTO probe SELECT g, g % 9 FROM generate_series(1, 20000) g;
		CREATE INDEX probe_b_idx ON probe (b);
		ALTER TABLE probe SET (autovacuum_enabled = false)`)

	// The transaction already in the database, held open on one connection until the
	// rebuild is proved to be waiting behind it.
	blocker, err := admin.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	exec(t, t.Context(), blocker, "BEGIN; SELECT count(*) FROM probe")

	lock := 500 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Minute)
	defer cancel()
	applied := make(chan error, 1)
	go func() {
		applied <- db.MigrateWith(ctx, migrateURL, db.MigrationBudget{LockTimeout: &lock},
			db.MigrationSource{Owner: "waiter", Files: fstest.MapFS{
				"000001_reindex.up.sql": {Data: []byte("-- pkit: autocommit=true\n" +
					"REINDEX (CONCURRENTLY) TABLE probe")},
			}})
	}()

	// The watch waits from here, so its window covers the patient wait for the
	// composition lock that comes before the rebuild — which every other package's boot
	// migration is queuing on too. What the case measures stays relative to the moment
	// the rebuild is *seen*.
	if !watchForStatement(t, dbtest.Open(t, migrateURL), "REINDEX (CONCURRENTLY) TABLE probe", 3*time.Minute) {
		t.Fatal("the run was never seen inside its concurrent rebuild, so this case measured nothing")
	}
	// The run's budget is now some times exceeded while the reader it waits behind is
	// still open. What the run did in that span is the whole question.
	time.Sleep(4 * lock)
	exec(t, t.Context(), blocker, "COMMIT")

	switch err := <-applied; {
	case errors.Is(err, db.ErrContended):
		t.Errorf("the autocommit statement was refused for waiting, at the run's own %s budget: %v — "+
			"waiting for the transactions already in the database is what CONCURRENTLY is for, and "+
			"ADR 0011 bounds none of that wait; cut short, the rebuild also leaves what it built behind",
			lock, err)
	case err != nil:
		t.Fatalf("the run refused: %v", err)
	}
	// The damage a cancelled rebuild leaves, named rather than inferred from a log line:
	// the new copy of an index it was midway through, which no later run has any way to
	// tell apart from one it is about to build.
	if left := countRows(t, admin, "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace"+
		" WHERE n.nspname = current_schema() AND c.relname LIKE '%\\_ccnew%' ESCAPE '\\'"); left != 0 {
		t.Errorf("%d half-built index copies left behind by the run: the rebuild was cut short, not refused", left)
	}
	var invalid int
	scan(t, admin, "SELECT count(*) FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid"+
		" JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = current_schema() AND NOT i.indisvalid",
		&invalid)
	if invalid != 0 {
		t.Errorf("%d invalid indexes after the run: the rebuild was cut short rather than left to finish", invalid)
	}
}

// watchForStatement says whether a backend of this database was seen running a statement
// that contains subject, within wait. It reads `pg_stat_activity` because that is the only
// account of what a session is doing right now — the same view the rehearsal samples.
func watchForStatement(t *testing.T, watch *sql.DB, subject string, wait time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(wait)
	for {
		var running int
		scan(t, watch, "SELECT count(*) FROM pg_stat_activity WHERE datname = current_database()"+
			" AND pid <> pg_backend_pid() AND state = 'active' AND query LIKE '%"+subject+"%'", &running)
		if running > 0 {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-t.Context().Done():
			return false
		case <-time.After(5 * time.Millisecond):
		}
	}
}
