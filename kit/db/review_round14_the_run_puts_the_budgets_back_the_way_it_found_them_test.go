package db_test

// review_round14_the_run_puts_the_budgets_back_the_way_it_found_them_test.go is review
// round 14's pin over b4b68ee ("a nontransactional file waits its own wait; the lock
// budget does not reach it").
//
// The commit draws a line and then trusts three sentences of prose to keep it. ADR 0011
// says of a nontransactional statement that "no `lock_timeout` bounds the wait", so that
// statement now runs under `SET lock_timeout TO '0'` (runner.noLockBudget) with the
// statement budget as configured; the queue for the composition lock has both budgets
// taken off for the wait (runner.unbudgeted); and "budgets() still runs before every file
// and every batch, so nothing the patience touches leaks". All three are claims about what
// one session is set to at a given moment, and no case in the tree read a session across
// that boundary: the branch's own new file asks the index and the clock, and
// review_guarantees_test.go reads current_setting only inside a transactional file with no
// autocommit file in front of it.
//
// So this case reads the session the way the kernel's own case does — from inside the run,
// by the file that has to be answered for — across one run of three files, with a reader
// holding a snapshot of the table so the nontransactional file is genuinely made to wait:
//
//   - the nontransactional file must wait the reader out and apply. That is ADR 0011's
//     half, and it is what CI refused 93bf75dc for; at the commit before this one the run
//     stops here with 55P03 at the 250ms it was handed, so this case is red on that tree.
//   - the transactional file after it must find both budgets back on the session, exactly
//     as the deployment named them — the lock budget the run was handed, and the statement
//     budget it was handed — because the patience was bought for one statement and not for
//     the session. A leak is what "nothing the patience touches leaks" denies, and it is
//     the assertion that has no other owner: drop the r.budgets(ctx) at the end of
//     holdCompositionLock or the one at the top of apply and this file is the thing that
//     says so.
//
// A file that finds its session set to something else raises, and the message names what
// it found. The probe sits in the third file rather than the second because the mode's own
// file cannot hold one: the runner hands a file's whole text to one ExecContext, and two
// statements in one simple query run inside an implicit transaction block, which
// CREATE INDEX CONCURRENTLY refuses (SQLSTATE 25001). That is worth knowing on its own and
// is said in the review.

import (
	"context"
	"testing"
	"testing/fstest"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// holdTableSnapshot opens one transaction that reads table and stays inside it for a
// moment, so a build started after the read has to outlive the read. Statement by
// statement on one session, because the snapshot a transaction in the default isolation
// level holds is taken per statement: a connection that merely sat idle would hold a
// snapshot newer than the build's and be nobody's wait.
func holdTableSnapshot(t *testing.T, url, table string) {
	t.Helper()
	holder := dbtest.Open(t, url)
	read := make(chan struct{})
	go func() {
		ctx := context.WithoutCancel(t.Context())
		conn, err := holder.Conn(ctx)
		if err != nil {
			close(read)
			return
		}
		defer conn.Close()
		if _, err := conn.ExecContext(ctx, "BEGIN"); err != nil {
			close(read)
			return
		}
		if _, err := conn.ExecContext(ctx, "SELECT count(*) FROM "+table); err != nil {
			close(read)
			return
		}
		close(read)
		_, _ = conn.ExecContext(ctx, "SELECT pg_sleep(1)")
		_, _ = conn.ExecContext(ctx, "ROLLBACK")
	}()
	<-read
}

func TestTheRunPutsTheBudgetsBackOnTheSessionTheWayItFoundThem(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	created := &fstest.MapFile{Data: []byte("CREATE TABLE border (id bigint PRIMARY KEY, b bigint)")}
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "border", Files: fstest.MapFS{
		"000001_border.up.sql": created,
	}}); err != nil {
		t.Fatalf("the file that created the table was refused: %v", err)
	}
	holdTableSnapshot(t, migrateURL, "border")

	// What the file after the nontransactional one asks of its own session. The words
	// below name no SQL keyword a rule reading could mistake for a statement.
	const after = `DO $$ BEGIN
  IF current_setting('lock_timeout') <> '250ms' THEN
    RAISE EXCEPTION 'the file after a nontransactional one ran under lock_timeout %; the run named 250ms, so the patience leaked', current_setting('lock_timeout');
  END IF;
  IF current_setting('statement_timeout') = '0' THEN
    RAISE EXCEPTION 'the file after a nontransactional one ran with no statement budget at all (%); the run named one', current_setting('statement_timeout');
  END IF;
END $$;
CREATE TABLE border_later (x text)`

	lock := 250 * time.Millisecond
	statement := 30 * time.Second
	err := db.MigrateWith(t.Context(), migrateURL, db.MigrationBudget{LockTimeout: &lock, StatementTimeout: &statement},
		db.MigrationSource{Owner: "border", Files: fstest.MapFS{
			"000001_border.up.sql": created,
			"000002_index.up.sql":  {Data: []byte("-- pkit: autocommit=true\nCREATE INDEX CONCURRENTLY IF NOT EXISTS border_b ON border (b)")},
			"000003_after.up.sql":  {Data: []byte(after)},
		}})
	if err != nil {
		t.Fatalf("the run refused: %v\n"+
			"the run was handed a 250ms lock budget and a 30s statement budget. The nontransactional file is owed "+
			"neither number for its wait — ADR 0011: no lock budget bounds it — and must wait the reader out; the "+
			"file after it is owed both numbers back.", err)
	}

	// The nontransactional file applied, as a working index.
	// Names resolved through search_path, which is the test's own schema: a catalogue row
	// matched on a bare name belongs to whichever case first created it (that is what
	// TestACatalogCountOfOneTestsOwnSchemaNamesItsSchema refuses).
	var valid bool
	if qerr := dbtest.Open(t, migrateURL).QueryRowContext(t.Context(),
		"SELECT i.indisvalid FROM pg_index i WHERE i.indexrelid = to_regclass('border_b')").Scan(&valid); qerr != nil {
		t.Fatalf("the nontransactional file applied but left no index to ask about: %v", qerr)
	}
	if !valid {
		t.Error("border_b is invalid: the run reported a build as applied that did not produce a usable index")
	}
	// And the ordinary file after it reached the table it exists to create.
	var later int
	if qerr := dbtest.Open(t, migrateURL).QueryRowContext(t.Context(),
		"SELECT count(*) FROM pg_class WHERE oid = to_regclass('border_later')").Scan(&later); qerr != nil {
		t.Fatal(qerr)
	}
	if later != 1 {
		t.Errorf("the file after the nontransactional one left %d border_later tables; it should have applied like any other", later)
	}
}
