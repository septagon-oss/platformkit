package db_test

// data_file_shape_test.go is the delivery's case for the half of a refused data
// file that the third review's case cannot ask: which door converges it afterwards.
//
// A `phase=data` body of two statements is refused by the executor, because the
// window wraps one body, and the refusal now runs before the progress row exists: a
// row in `schema_migration_backfill` is what every later run reads as "this drain
// started, resume it", and a body that never ran once is not that.
//
// What follows is the ordering rule, and it is the reason the corrected file cannot
// converge in the migration that carried it. The first run applied the owner's
// schema file, so the owner has history, so the data file is a drain over a table
// with readers and `Migrate` leaves it: `db.Backfill` — the body of
// `jobs.BackfillMigrations` — is the door that empties the table, once, with nothing
// left in either of the runner's two tables.

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

func TestARefusedDataFileIsDrainedByTheWorkerOnceItIsCorrected(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	source := func(body string) db.MigrationSource {
		return db.MigrationSource{Owner: "twostep", Files: fstest.MapFS{
			"000001_probe.up.sql": {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, done boolean NOT NULL DEFAULT false);\nINSERT INTO probe (id) SELECT g FROM generate_series(1, 25) g")},
			"000002_fill.up.sql":  {Data: []byte(body)},
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

	// The remedy the refusal names — split the file — on the same version. The owner
	// has history now, so the migration still will not drain it.
	if err := db.Migrate(t.Context(), migrateURL, source(oneStatementData)); err != nil {
		t.Fatalf("the corrected file: %v", err)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE done"); n != 0 {
		t.Errorf("%d rows drained by a migration whose owner already has history; that table has readers and the worker owns its drain", n)
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
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'twostep' AND version = 2"); n != 1 {
		t.Errorf("the finished drain wrote %d history rows", n)
	}
}
