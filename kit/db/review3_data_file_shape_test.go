package db_test

// review3_data_file_shape_test.go is the third review's case for the one refusal
// of a phase=data file that the rule table does not make.
//
// The guard is what refuses "before the runner connects: an invalid later file
// must not let an earlier one change the schema" (kit/db/migration_files.go). A
// data file is judged against three rules and none of them is about the file
// having more than one statement — `data-with-ddl` fires only when a statement
// starts with ALTER, CREATE or DROP — so a body of two bounded UPDATEs passes
// every rule, is applied to the connection, and is refused afterwards by
// `drain` ("a data file is one statement … split the file"). By then
// `beginDrain` has written the progress row, and a row in
// `schema_migration_backfill` is precisely the state kit/db reads as "this
// drain started, resume it": planOwner stops treating the file as one that
// never ran and treats every later run as that resume, and every tick of
// jobs.BackfillMigrations errors on the same refusal.
//
// What the review's case for it — the same review as
// kit/db/review_rules_test.go — left out: the executor's refusal is real, so
// what this asserts is only what the README promises about a refusal, that it
// writes nothing resumable, and that the remedy the message names converges.
//
// The evidence that this refusal is not the guard's (measured, and the reason
// the file's own earlier statement reached the schema): the same source against
// an address that answers nothing returns
//
//	db: migrate: connect: failed to connect to `user=nobody database=platformkit`: …
//
// while every file the rule table refuses answers with its rule from that same
// address. That half belongs in the report, not here: refusing the shape in the
// rule table and refusing it later in the executor are both defensible, and only
// one of them is the smallest fix.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// twoStatements is the shape nothing refuses before the server does: both bodies
// read the window, so neither is unbounded, and neither is DDL.
const twoStatements = `-- pkit: phase=data
-- pkit: batch=10
-- pkit: table=probe
UPDATE probe SET passes = passes + 1 WHERE id IN (SELECT id FROM batch);
UPDATE probe SET done = true WHERE id IN (SELECT id FROM batch)`

func twoStatementSource(body string) db.MigrationSource {
	return db.MigrationSource{Owner: "shape", Files: fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte(`CREATE TABLE probe (id bigint PRIMARY KEY, done boolean NOT NULL DEFAULT false, passes integer NOT NULL DEFAULT 0);
INSERT INTO probe (id) SELECT g FROM generate_series(1, 25) g`)},
		"000002_two.up.sql": {Data: []byte(body)},
	}}
}

// TestARefusedDataFileShapeLeavesNoDrainBehind.
func TestARefusedDataFileShapeLeavesNoDrainBehind(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	err := db.Migrate(t.Context(), migrateURL, twoStatementSource(twoStatements))
	if err == nil {
		t.Fatal("a phase=data file with two statements reported success; the drain wraps one body in a window, so it has to refuse two")
	}
	if !strings.Contains(err.Error(), "shape/000002_two.up.sql") {
		t.Errorf("the refusal %q does not name the file whose shape it refuses", err)
	}
	admin := dbtest.Open(t, migrateURL)
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'shape' AND version = 2"); n != 0 {
		t.Errorf("the refused file wrote %d history rows; the version never ran", n)
	}
	// This is the row the case exists for. Nothing drained — the body never ran
	// once — and the table that says where a drain restarts holds a row for it.
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
		t.Errorf("the refused run left %d drain progress row(s) behind; no row was ever written, and kit/db reads that row as a drain that started", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE passes <> 0"); n != 0 {
		t.Errorf("the refused run wrote %d rows", n)
	}

	// The remedy the message names — split the file — converges on the same
	// schema, with every row written exactly once and no progress left behind.
	if err := db.Migrate(t.Context(), migrateURL, twoStatementSource(
		`-- pkit: phase=data
-- pkit: batch=10
-- pkit: table=probe
UPDATE probe SET passes = passes + 1, done = true WHERE id IN (SELECT id FROM batch)`)); err != nil {
		t.Fatalf("the corrected file: %v", err)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE passes <> 1 OR done IS NOT TRUE"); n != 0 {
		t.Errorf("%d rows are not the way the corrected drain leaves them", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
		t.Errorf("%d progress row(s) outlived the drain", n)
	}
}
