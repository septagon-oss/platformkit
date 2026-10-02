package db_test

import (
	"context"
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
// What this case adds is the leg that was red: the exception marker accepted, the
// file applied, and the wait supplied by the thing a concurrent build exists to wait
// for — one session holding nothing but an open snapshot on the table. The case names
// no budget, so what is under test is the product's own default: hold the reader for
// eight seconds, and a run that still carried its lock budget into the statement dies
// at five, which is what measured here once that window is put back on the session —
// `failed after 5.094s: … canceling statement due to lock timeout (SQLSTATE 55P03)`,
// run 189's line word for word. Applied here as the product stands, the same run took
// 7.5s and left one valid index. No bound of this case is widened to make it pass.
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
	holder := dbtest.Open(t, migrateURL)
	conn, err := holder.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(t.Context(), "BEGIN"); err != nil {
		t.Fatal(err)
	}
	reader, cancelReader := context.WithCancel(context.WithoutCancel(t.Context()))
	defer cancelReader()
	go func() {
		if _, err := conn.ExecContext(reader, "SELECT count(*) FROM probe; SELECT pg_sleep(8)"); err != nil {
			return
		}
		_, _ = conn.ExecContext(reader, "COMMIT")
	}()
	// The reader is seen, not assumed: until it is asleep inside its transaction there
	// is no snapshot for the build to wait for, and the case would measure nothing.
	admin := dbtest.Open(t, migrateURL)
	for {
		var sleeping bool
		scan(t, admin, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
			WHERE application_name = current_setting('search_path') AND wait_event = 'PgSleep')`, &sleeping)
		if sleeping {
			break
		}
		select {
		case <-time.After(10 * time.Millisecond):
		case <-t.Context().Done():
			t.Fatal("the reader never reached its sleep")
		}
	}

	started := time.Now()
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "probe", Files: fstest.MapFS{
		"000001_probe.up.sql": creates,
		"000002_rule.up.sql": {Data: []byte(
			"-- pkit: autocommit=true\nCREATE INDEX CONCURRENTLY IF NOT EXISTS probe_b ON probe (b)")},
	}}); err != nil {
		t.Fatalf("the exception leg did not except the rule after %s: %v", time.Since(started).Truncate(time.Millisecond), err)
	}
	waited := time.Since(started)
	if waited < 5*time.Second {
		t.Errorf("the allow leg finished in %s, quicker than the reader it was given: the rebuild waited for nothing, which is the other way this case can be wrong",
			waited.Truncate(time.Millisecond))
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
	t.Logf("the allow leg applied in %s while a reader held an open snapshot on the table", waited.Truncate(time.Millisecond))
}
