package db_test

// migrate_autocommit_budget_test.go pins the two waits a migration run makes, which
// are not the same wait and are not bounded by the same thing.
//
// The lock budget is the runner's patience behind a lock that stops writers: five
// seconds by default, and the file that cannot get one is refused as ErrContended with
// nothing applied. A nontransactional file — `-- pkit: autocommit=true`, the mode the
// rule table forces a CONCURRENTLY build into — waits for something else entirely: for
// the transactions already in the database to stop holding a snapshot of the table it
// is building on. That wait is the statement's own work, which is why the README says
// the *duration* bound is off by default for exactly this statement, and why kit/db's
// own comment claimed no lock_timeout bounds that wait. It was the runner's promise and
// not the server's: measured on PostgreSQL 16, a CONCURRENTLY build that finds one
// session holding an older snapshot is cancelled at 55P03 by lock_timeout — the build
// takes a lock on each transaction it has to outlive — and what it leaves behind is an
// INVALID index, which the `IF NOT EXISTS` the rule table demands of this mode then
// excuses on the retry the refusal tells the operator to run: the rerun reports the file
// applied and the index it exists to build does not exist. So the first case is the wait
// the mode exists to make, and the second is the wait the budget exists to refuse.

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// holdSnapshot runs one transaction on a connection of its own that reads table and
// keeps the snapshot it took for about a second. The caller is told once the read has
// happened, so a build that starts after the signal waits for a snapshot rather than
// racing with one. The pause is a statement and not a Go timer because a transaction in
// the default isolation level takes its snapshot per statement: a connection that was
// idle for a second holds an xmin a second newer than the build's and is nobody's wait.
func holdSnapshot(t *testing.T, url, table string) {
	t.Helper()
	holder := dbtest.Open(t, url)
	held := make(chan struct{})
	go func() {
		ctx := context.WithoutCancel(t.Context())
		conn, err := holder.Conn(ctx)
		if err != nil {
			close(held)
			return
		}
		defer conn.Close()
		// One statement per call on one session: the transaction stays open across them,
		// which a multi-statement Exec would not do, since that runs inside a transaction
		// of the driver's own and commits it when the call returns.
		if _, err := conn.ExecContext(ctx, "BEGIN"); err != nil {
			close(held)
			return
		}
		if _, err := conn.ExecContext(ctx, "SELECT count(*) FROM "+table); err != nil {
			close(held)
			return
		}
		close(held)
		_, _ = conn.ExecContext(ctx, "SELECT pg_sleep(1)")
		_, _ = conn.ExecContext(ctx, "ROLLBACK")
	}()
	<-held
}

// TestANontransactionalFileWaitsForTheSessionsAlreadyInTheDatabase is the CI failure: a
// CONCURRENTLY build whose table one reader is holding a snapshot of, on a run configured
// with a lock budget far shorter than that read. The build waits the read out and applies.
func TestANontransactionalFileWaitsForTheSessionsAlreadyInTheDatabase(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	first := &fstest.MapFile{Data: []byte("CREATE TABLE patience (id bigint PRIMARY KEY, b bigint)")}
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "patience", Files: fstest.MapFS{
		"000001_probe.up.sql": first,
	}}); err != nil {
		t.Fatalf("the file that created the table was refused: %v", err)
	}
	holdSnapshot(t, migrateURL, "patience")

	lock := 100 * time.Millisecond
	started := time.Now()
	err := db.MigrateWith(t.Context(), migrateURL, db.MigrationBudget{LockTimeout: &lock},
		db.MigrationSource{Owner: "patience", Files: fstest.MapFS{
			"000001_probe.up.sql": first,
			"000002_index.up.sql": {Data: []byte("-- pkit: autocommit=true\nCREATE INDEX CONCURRENTLY IF NOT EXISTS patience_b ON patience (b)")},
		}})
	waited := time.Since(started)
	if err != nil {
		t.Fatalf("the autocommit file was refused while a reader held a snapshot: %v (waited %s on a %s lock budget); "+
			"the wait a CONCURRENTLY build makes is the statement's own work, and the runner's comment says no lock budget "+
			"bounds it — leaving an invalid index behind and telling the operator to run the file again", err,
			waited.Round(time.Millisecond), lock)
	}
	// A run that never had to wait would say nothing, so the wait is part of the claim.
	if waited < 200*time.Millisecond {
		t.Errorf("the build finished in %s, which is faster than the read it was made to outlive; this case asked it nothing",
			waited.Round(time.Millisecond))
	}
	// Applied, and applied as a working index: the point of the file, and the thing a
	// cancelled build leaves as indisvalid = false for a rerun that then skips.
	var valid bool
	if err := dbtest.Open(t, migrateURL).QueryRowContext(t.Context(),
		"SELECT i.indisvalid FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid WHERE c.relname = 'patience_b'").Scan(&valid); err != nil {
		t.Fatalf("the file applied but no index is there to ask about: %v", err)
	}
	if !valid {
		t.Error("patience_b exists and is invalid: the build was cancelled partway, which is the state a rerun of an IF NOT EXISTS file excuses")
	}
}

// TestTheLockBudgetStillRefusesAFileThatMustQueueBehindAWriter is the other half, and the
// reason the cure above is not a loosened budget: a transactional file that needs the
// table from a writer is still refused at the same number, with the same error, and applied
// none of itself.
func TestTheLockBudgetStillRefusesAFileThatMustQueueBehindAWriter(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	first := &fstest.MapFile{Data: []byte("CREATE TABLE queued (id bigint PRIMARY KEY, b bigint); INSERT INTO queued (id, b) VALUES (1, 1)")}
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "queued", Files: fstest.MapFS{
		"000001_probe.up.sql": first,
	}}); err != nil {
		t.Fatalf("the file that created the table was refused: %v", err)
	}

	// A writer holding one row of the table: what an ADD COLUMN has to queue behind. It
	// gives the row back once the run is queued behind it, which is the only moment the
	// claim needs it held for — see releaseWhenQueued.
	holder := dbtest.Open(t, migrateURL)
	conn, err := holder.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx := context.WithoutCancel(t.Context())
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("the writer could not open a transaction: %v", err)
	}
	var id int64
	var writer int64
	if err := tx.QueryRowContext(ctx, "SELECT id, pg_backend_pid() FROM queued WHERE id = 1 FOR UPDATE").Scan(&id, &writer); err != nil {
		t.Fatalf("the writer could not take the row: %v", err)
	}

	lock := 100 * time.Millisecond
	refused := make(chan error, 1)
	go func() {
		refused <- db.MigrateWith(ctx, migrateURL, db.MigrationBudget{LockTimeout: &lock},
			db.MigrationSource{Owner: "queued", Files: fstest.MapFS{
				"000001_probe.up.sql":  first,
				"000002_column.up.sql": {Data: []byte("ALTER TABLE queued ADD COLUMN c text")},
			}})
	}()
	err = releaseWhenQueued(t, holder, refused, tx.Rollback, lock, writer)
	if !errors.Is(err, db.ErrContended) {
		t.Errorf("a file that could not take the table lock within %s returned %v; that wait is the one the lock budget exists to refuse", lock, err)
	}
	var columns int
	if err := holder.QueryRowContext(t.Context(),
		"SELECT count(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'queued' AND column_name = 'c'").Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if columns != 0 {
		t.Errorf("the refused file added its column anyway: the run that reports a contention applies nothing it had not already applied")
	}
}

// releaseWhenQueued runs the file's error back and gives the held row back at the one
// moment that keeps the refusal honest: once this run is sitting, ungranted, on this
// table's ACCESS EXCLUSIVE lock behind this writer, ten budgets' length later at the
// latest.
//
// A fixed pause on the Go side was the earlier shape and it was a coin toss. Before a
// file is applied the run queues for the composition's advisory lock, and that queue is
// patient by design and holds nothing back (see holdCompositionLock); the lock is keyed
// per database, and every other test of every package here migrates against one. So the
// run can arrive at its ALTER long after the Go-side pause ended and apply the file this
// case exists to see refused — measured: <nil> after 2.017s, with the column present.
//
// Waiting for "a queue on the table" was the next shape, and it read the wrong session:
// pg_locks is a cluster-wide view, so a lock wait in another database, or another run's
// wait for another table whose relation OID is the one to_regclass returns here, releases
// the writer while this run is still queueing for the advisory lock — and the ALTER then
// finds the row free. The queue is therefore asked for by name: the waiting session's own
// pg_blocking_pids must contain the writer's backend, which is this test's writer and
// nobody else's. Nothing but this run being blocked behind this held row releases it.
//
// Releasing once the budget has had ten times its own length to refuse the wait is what
// still answers for the budget: a run left without one lets the ALTER through and this
// case says so with the file applied, rather than waiting for the package's own timeout.
func releaseWhenQueued(t *testing.T, admin *sql.DB, refused <-chan error, release func() error, budget time.Duration, writer int64) error {
	t.Helper()
	// to_regclass and not a bare relname: the queue has to be this test's table, in the
	// schema the connection URL carries, and another test's schema may name the table the
	// same. A granted lock is not a queue, and a queue that does not name this writer is
	// somebody else's wait, so both are excluded.
	const queued = `SELECT count(*) FROM pg_locks l
		WHERE l.locktype = 'relation' AND NOT l.granted AND l.mode = 'AccessExclusiveLock'
		  AND l.relation = to_regclass('queued')
		  AND l.database = (SELECT oid FROM pg_database WHERE datname = current_database())
		  AND l.pid <> $1
		  AND pg_blocking_pids(l.pid) @> ARRAY[$1::integer]`
	for {
		select {
		case err := <-refused:
			return err
		default:
		}
		var waiting int
		if err := admin.QueryRowContext(t.Context(), queued, writer).Scan(&waiting); err != nil {
			t.Fatalf("the run's queue behind the writer could not be read: %v", err)
		}
		if waiting > 0 {
			time.Sleep(10 * budget)
			if err := release(); err != nil {
				t.Errorf("the writer could not give the row back: %v", err)
			}
			return <-refused
		}
		time.Sleep(10 * time.Millisecond)
	}
}
