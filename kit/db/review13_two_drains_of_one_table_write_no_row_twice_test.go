package db_test

// review13_two_drains_of_one_table_write_no_row_twice_test.go is the thirteenth round's pin
// of the one claim the drain makes about a second runner.
//
// `Backfill` deliberately does not take the advisory lock every migration takes: a drain
// that held it across a ten-million-row table would stop every other role's boot. What the
// code says instead is that "the write that protects a drain from a second runner is the
// compare-and-set on the cursor rather than a lock held across the work". No case in this
// package runs two drains of one table at once, and the two-runner case that does exist
// (`kit/db/migrate_test.go`'s TestConcurrentMigrationsApplyEachFileOnce) runs the *lock*
// path, which is a different mechanism.
//
// So this runs two of them against one table, in one schema, at the same moment, with a
// window small enough that their windows overlap: whatever else the two runs decide, no row
// is written twice, the version applies once, and the run that loses the compare-and-set
// leaves no half-written batch behind. The assertions are on the table and the ledger, never
// on which goroutine won — a schedule that lets one run finish before the other starts is
// legal, and both returning nil then is correct.

import (
	"sync"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestTwoDrainsOfOneTableWriteNoRowTwice(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	files := fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte(`CREATE TABLE probe (id bigint PRIMARY KEY, passes integer NOT NULL DEFAULT 0);
INSERT INTO probe (id) SELECT g FROM generate_series(1, 100) g`)},
	}
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "tworunners", Files: files}); err != nil {
		t.Fatal(err)
	}
	files["000002_mark.up.sql"] = &fstest.MapFile{Data: []byte(`-- pkit: phase=data
-- pkit: batch=2
-- pkit: table=probe
UPDATE probe SET passes = passes + 1 WHERE id IN (SELECT id FROM batch)`)}
	source := db.MigrationSource{Owner: "tworunners", Files: files}

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = db.Backfill(t.Context(), migrateURL, source)
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		t.Logf("drain %d returned %v", i, err)
	}

	admin := dbtest.Open(t, migrateURL)
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE passes > 1"); n != 0 {
		t.Errorf("%d rows were written more than once by two drains of one table: the compare-and-set on the cursor is the only thing between them", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'tworunners' AND version = 2"); n > 1 {
		t.Errorf("%d history rows for one data file", n)
	}

	// Whatever the two runs left, a third drain finishes the file: the work is resumable
	// from the cursor, and a lost race is not a drain that has to be undone.
	if err := db.Backfill(t.Context(), migrateURL, source); err != nil {
		t.Fatalf("the drain that finishes what the two races left: %v", err)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE passes = 1"); n != 100 {
		t.Errorf("%d of 100 rows drained exactly once", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE passes > 1"); n != 0 {
		t.Errorf("%d rows carry more than one write after the drain finished", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
		t.Errorf("%d progress rows outlive the finished drain", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'tworunners' AND version = 2"); n != 1 {
		t.Errorf("%d history rows for the drained file, want exactly 1", n)
	}
}
