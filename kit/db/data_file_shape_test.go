package db_test

// data_file_shape_test.go is the delivery's case for the two drains a migration run
// does not own, beside the one it does.
//
// A `phase=data` body of two statements is refused by the executor, because the window
// wraps one body, and the refusal runs before the progress row exists: that row is what
// every later run reads as "this drain started, resume it", and a body that never ran
// once is not that. kit/db/review3_data_file_shape_test.go holds that refusal and the
// corrected file converging through `Migrate` — the owner's last pending file, with
// nothing of its owner waiting behind it, so there is nothing to keep in order and the
// run has a window to bound the work with.
//
// What follows is the other half of the same rule, which that case cannot reach: the
// drain with a file behind it, which belongs to the worker because the release cannot
// complete in this run either way, and the drain no window bounds at all, which belongs
// to the worker whatever sits behind it — `allow=data-body-unbounded` says the body
// bounds itself, so the fifty batches a migration gives itself are not a bound this run
// can hold it to.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

const twoStatementData = `-- pkit: phase=data
-- pkit: batch=10
-- pkit: table=probe
UPDATE probe SET done = true WHERE id IN (SELECT id FROM batch);
SELECT 1`

const oneStatementData = `-- pkit: phase=data
-- pkit: batch=10
-- pkit: table=probe
UPDATE probe SET done = true WHERE id IN (SELECT id FROM batch)`

// TestARefusedDataFileIsDrainedByTheWorkerOnceItIsCorrected walks the convergence of a
// refused data file that has a schema file waiting behind it: the refusal leaves nothing
// resumable, the corrected file is the worker's to drain because version 3 waits for it,
// and the migration after the drain finds nothing in its way.
func TestARefusedDataFileIsDrainedByTheWorkerOnceItIsCorrected(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	source := func(body string) db.MigrationSource {
		return db.MigrationSource{Owner: "twostep", Files: fstest.MapFS{
			"000001_probe.up.sql": {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, done boolean NOT NULL DEFAULT false);\nINSERT INTO probe (id) SELECT g FROM generate_series(1, 25) g")},
			"000002_fill.up.sql":  {Data: []byte(body)},
			"000003_note.up.sql":  {Data: []byte("ALTER TABLE probe ADD COLUMN note text")},
		}}
	}
	err := db.Migrate(t.Context(), migrateURL, source(twoStatementData))
	if err == nil || !strings.Contains(err.Error(), "twostep/000002_fill.up.sql") {
		t.Fatalf("a two-statement data file reported %v; the window wraps one body, so the file is refused and has to name itself", err)
	}
	admin := dbtest.Open(t, migrateURL)
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
		t.Fatalf("%d progress row(s) survive a file that never ran; kit/db reads that row as a drain to resume, and every tick of the worker would answer it with this same refusal", n)
	}
	if cols := countRows(t, admin, "SELECT count(*) FROM pg_attribute WHERE attrelid = 'probe'::regclass AND attname = 'note'"); cols != 0 {
		t.Errorf("the file behind the refused one applied anyway; the run stops at the data file it cannot drain")
	}

	// The remedy the refusal names — split the file — on the same version. The owner has
	// history now, and 000003 waits behind the drain, so the migration still will not
	// drain it: the release cannot reach that ALTER in this run either way.
	if err := db.Migrate(t.Context(), migrateURL, source(oneStatementData)); err != nil {
		t.Fatalf("the corrected file: %v", err)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE done"); n != 0 {
		t.Errorf("%d rows drained by a migration with a file waiting behind its data file; the release cannot complete here, and the worker owns the drain", n)
	}
	if err := db.Backfill(t.Context(), migrateURL, source(oneStatementData)); err != nil {
		t.Fatalf("the worker's door: %v", err)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE NOT done"); n != 0 {
		t.Errorf("%d rows were left undrained by the door that drains them", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
		t.Errorf("%d progress row(s) outlived the drain", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'twostep' AND version = 3"); n != 0 {
		t.Errorf("the drain wrote %d history rows for the file behind it; that version is the next migration's", n)
	}
	if err := db.Migrate(t.Context(), migrateURL, source(oneStatementData)); err != nil {
		t.Fatalf("the migration after the drain: %v", err)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM pg_attribute WHERE attrelid = 'probe'::regclass AND attname = 'note'"); n != 1 {
		t.Errorf("the release behind a finished drain still did not apply (%d column(s))", n)
	}
}

// TestABodyThatBoundsItselfWaitsForTheDoorThatCanBoundNothing is the windowed half of
// the last-pending-file rule. A file the owner left nothing behind is drained by the
// migration, bounded; a body that declared itself bounded has no window, so the bound a
// migration gives itself — a count of windows — is nothing it can hold that body to, and
// an unbounded statement over a table with readers is the worker's even when it is last.
func TestABodyThatBoundsItselfWaitsForTheDoorThatCanBoundNothing(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	files := fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, passes integer NOT NULL DEFAULT 0);\nINSERT INTO probe (id) SELECT g FROM generate_series(1, 25) g")},
	}
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "self", Files: files}); err != nil {
		t.Fatal(err)
	}
	files["000002_mark.up.sql"] = &fstest.MapFile{Data: []byte(`-- pkit: phase=data
-- pkit: batch=10
-- pkit: table=probe
-- pkit: allow=data-body-unbounded reason=one statement gives every row the same value
UPDATE probe SET passes = passes + 1`)}
	source := db.MigrationSource{Owner: "self", Files: files}
	if err := db.Migrate(t.Context(), migrateURL, source); err != nil {
		t.Fatalf("a self-bounded data file: %v", err)
	}
	admin := dbtest.Open(t, migrateURL)
	if n := countRows(t, admin, "SELECT coalesce(sum(passes),0) FROM probe"); n != 0 {
		t.Errorf("%d rows were written by a boot that has no bound to hold this body to; the drain it cannot bound is the worker's", n)
	}
	if err := db.Backfill(t.Context(), migrateURL, source); err != nil {
		t.Fatalf("the worker's door: %v", err)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE passes <> 1"); n != 0 {
		t.Errorf("%d of 25 rows were not written exactly once by the door that runs a self-bounded body", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'self' AND version = 2"); n != 1 {
		t.Errorf("the finished drain wrote %d history rows", n)
	}
}
