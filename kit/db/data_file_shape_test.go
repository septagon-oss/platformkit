package db_test

// The shape a `phase=data` file has to have to be drained at all, and what a refusal of
// that shape leaves behind.
//
// The rule table refuses a data file whose statement starts with ALTER, CREATE or DROP,
// whose body never reads the window, or whose header will not parse. Nothing in it counts
// statements: `data-with-ddl` fires on a leading word only, so a body of two bounded
// UPDATEs passes every rule, is applied to the connection, and is refused afterwards by
// `drain` ("a data file is one statement … split the file"). By then `beginDrain` has
// written the progress row, and a row in `schema_migration_backfill` is precisely the
// state kit/db reads as "this drain started, resume it": planOwner stops treating the
// file as one that never ran and treats every later run as that resume, and every tick of
// jobs.BackfillMigrations answers with the same refusal. `Migrate` runs
// TestARefusedDataFileShapeLeavesNoDrainBehind's file as the owner's last pending file,
// with nothing of its owner waiting behind it, so there is nothing to keep in order and
// the run has a window to bound the work with.
//
// What the cases below reach that that one cannot: the drain with a file behind it,
// which belongs to the worker because the release cannot complete in this run either way,
// and the drain no window bounds at all, which belongs to the worker whatever sits behind
// it — `allow=data-body-unbounded` says the body bounds itself, so the fifty batches a
// migration gives itself are not a bound this run can hold it to.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// twoBoundedStatements is the shape nothing refuses before the server does: both bodies
// read the window, so neither is unbounded, and neither is DDL.
const twoBoundedStatements = `-- pkit: phase=data
-- pkit: batch=10
-- pkit: table=probe
UPDATE probe SET passes = passes + 1 WHERE id IN (SELECT id FROM batch);
UPDATE probe SET done = true WHERE id IN (SELECT id FROM batch)`

// twoBoundedStatementSource is that body as the owner's last pending file, which is the
// only shape a migration runs: with a file behind it the release cannot complete either
// way, and the drain is the worker's.
func twoBoundedStatementSource(body string) db.MigrationSource {
	return db.MigrationSource{Owner: "shape", Files: fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte(`CREATE TABLE probe (id bigint PRIMARY KEY, done boolean NOT NULL DEFAULT false, passes integer NOT NULL DEFAULT 0);
INSERT INTO probe (id) SELECT g FROM generate_series(1, 25) g`)},
		"000002_two.up.sql": {Data: []byte(body)},
	}}
}

// TestARefusedDataFileShapeLeavesNoDrainBehind is the one refusal of a `phase=data` file
// the rule table does not make, asserted through the rows a refused run leaves.
//
// The guard that refuses "an invalid later file must not let an earlier one change the
// schema" (kit/db/migration_files.go) reads a data file against three rules, none of them
// about how many statements it holds, so the refusal comes later, from `drain`, and by
// then `beginDrain` has written the progress row that kit/db reads as a drain to resume.
// What is asserted is the promise the README makes about a refusal — it leaves nothing
// resumable, no history row, no half-written batch — and that the remedy the message
// names, splitting the file, converges on the same schema with every row written once.
//
// What this case does not settle: the refusal is the executor's rather than the guard's.
// Measured, and the reason the file's own earlier statement reached the schema — the same
// source against an address that answers nothing returns
//
//	db: migrate: connect: failed to connect to `user=nobody database=platformkit`: …
//
// while every file the rule table refuses answers with its rule from that same address.
// Refusing this shape in the rule table and refusing it in the executor are both
// defensible; which one the runner answers with is not decided here.
func TestARefusedDataFileShapeLeavesNoDrainBehind(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	err := db.Migrate(t.Context(), migrateURL, twoBoundedStatementSource(twoBoundedStatements))
	if err == nil {
		t.Fatal("a phase=data file with two statements reported success; the drain wraps one body, so it has to refuse two")
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
	if err := db.Migrate(t.Context(), migrateURL, twoBoundedStatementSource(
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

// TestADataBodyThatOpensWithItsOwnCTEIsDrainedAsOneStatement is the wrapper's half of the
// promise that "a shape the window cannot run is refused before that row exists": the shape
// a backfill takes naturally — a named list of the rows it is about to touch, then the
// update — is not refused, it is drained, because PostgreSQL takes one WITH per statement
// and the kernel's window joins the body's own list rather than standing in front of it. The
// window leads the merged list, so a body CTE may read it, and RECURSIVE is a property of a
// list rather than of one member, so the body's word for it moves to the front.
func TestADataBodyThatOpensWithItsOwnCTEIsDrainedAsOneStatement(t *testing.T) {
	for _, tc := range []struct {
		name, body    string
		drained, kept int
	}{
		{"a plain list", `WITH stale AS (SELECT id FROM probe WHERE note = 'stale')
UPDATE probe SET note = 'done' WHERE id IN (SELECT id FROM stale INTERSECT SELECT id FROM batch)`, 6, 6},
		{"a recursive one", `WITH RECURSIVE stale_low AS (SELECT id FROM probe WHERE note = 'stale' AND id < 9)
UPDATE probe SET note = 'done' WHERE id IN (SELECT id FROM stale_low INTERSECT SELECT id FROM batch)`, 4, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{
				"000001_probe.up.sql": {Data: []byte(`CREATE TABLE probe (id bigint PRIMARY KEY, note text NOT NULL DEFAULT 'fresh');
INSERT INTO probe (id, note) SELECT g, CASE WHEN g % 2 = 0 THEN 'stale' ELSE 'fresh' END FROM generate_series(1, 12) g`)},
				"000002_fill.up.sql": {Data: []byte("-- pkit: phase=data\n-- pkit: batch=5\n-- pkit: table=probe\n" + tc.body)},
			}
			if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "ctebody", Files: files}); err != nil {
				t.Fatalf("the window answered a body that opens with its own list: %v", err)
			}
			admin := dbtest.Open(t, migrateURL)
			if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE note = 'done'"); n != tc.drained {
				t.Errorf("%d of the %d rows the body named drained: the body's own list and the kernel's window are one statement or neither", n, tc.drained)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE note = 'fresh'"); n != tc.kept {
				t.Errorf("%d rows the body excluded were written: the merged statement ran past the body's own filter", n)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'ctebody' AND version = 2"); n != 1 {
				t.Errorf("%d history rows for a drain that ran", n)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
				t.Errorf("%d progress rows outlive a drain that ran to the end of the table", n)
			}
		})
	}
}
