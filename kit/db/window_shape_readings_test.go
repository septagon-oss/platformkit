package db_test

// window_shape_readings_test.go pins the two readings the window makes of a body by its
// *constructs* rather than by one spelling of them, and the bound that ends a tick whose drain
// neither reading stopped — which, since the fourteenth round, is a drain that reaches the drained
// table through a view rather than one that names it and appends.
//
// The thirteenth review's first finding was that `batch` as a *binding* was refused by asking
// the shape for the four characters `batch as (` — so a quoted lower-case name (the same
// identifier to the server, and a name whose contents the shape puts away) and a name carrying
// its own column list both reached the server with two CTEs of one name, behind a progress row.
// Its third finding was a `phase=data` body that writes the column the cursor is ordered by: the
// rows it touches land above that cursor, so the table never empties and every window behind the
// first re-commits rows an earlier one wrote. Both are the same mistake — a rule that reads one
// spelling of a construct is a rule that reads the spellings it enumerated — and the answer
// under test here is a reading of the grammar position: the members of the CTE list the wrapper
// joins, and the assignment targets of the UPDATE that names the drained table.
//
// Every group therefore has both halves. What binds the window's name, writes its key or adds
// rows to the table it drains is refused whichever way it was written; what only *looks* like one
// of those shapes still drains, which is what keeps a guard that reads a construct from becoming a
// guard that matches more text. The last case asks what ends a drain neither reading stopped,
// because the answer has to be a number: the alternative is a tick that repeats work forever and
// never applies the version, holding the advisory lock that job's name elects on.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

const windowShapesProbe = `CREATE TABLE probe (id bigint PRIMARY KEY, note text NOT NULL DEFAULT '', passes integer NOT NULL DEFAULT 0);
INSERT INTO probe (id) SELECT g FROM generate_series(1, 12) g`

// windowShapesData is a phase=data file over the table above with one body.
func windowShapesData(body string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte("-- pkit: phase=data\n-- pkit: batch=5\n-- pkit: table=probe\n" + body)}
}

func windowShapesFiles(body string) fstest.MapFS {
	return fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte(windowShapesProbe)},
		"000002_fill.up.sql":  windowShapesData(body),
	}
}

// TestEverySpellingThatBindsTheWindowNameIsRefusedBeforeTheDrainStarts walks the CTE list the
// wrapper joins, one member per leg. Each leg refuses the binding of `batch` however the file
// wrote it and asserts the state the refusal promises and the server's own 42712 does not — no
// progress row, no row written, no history row — then drains the same body with its own
// relation named something else, which is the passing branch.
func TestEverySpellingThatBindsTheWindowNameIsRefusedBeforeTheDrainStarts(t *testing.T) {
	for _, tc := range []struct{ name, refused, corrected string }{
		{
			name: "the bare name, the one spelling the guard used to read",
			refused: `WITH batch AS (SELECT id FROM probe WHERE id < 7)
UPDATE probe SET note = 'done' WHERE id IN (SELECT id FROM batch)`,
			corrected: `WITH stale AS (SELECT id FROM probe WHERE id < 7)
UPDATE probe SET note = 'done' WHERE id IN (SELECT id FROM batch) AND id NOT IN (SELECT id FROM stale)`,
		},
		{
			name: "the name written quoted, which is the same identifier",
			refused: `WITH "batch" AS (SELECT id FROM probe WHERE id < 7)
UPDATE probe SET note = 'done' WHERE id IN (SELECT id FROM batch)`,
			corrected: `WITH "stale" AS (SELECT id FROM probe WHERE id < 7)
UPDATE probe SET note = 'done' WHERE id IN (SELECT id FROM batch) AND id NOT IN (SELECT id FROM "stale")`,
		},
		{
			name: "the name carrying its own column list",
			refused: `WITH batch (id) AS (SELECT id FROM probe WHERE id < 7)
UPDATE probe SET note = 'done' WHERE id IN (SELECT id FROM batch)`,
			corrected: `WITH stale (id) AS (SELECT id FROM probe WHERE id < 7)
UPDATE probe SET note = 'done' WHERE id IN (SELECT id FROM batch) AND id NOT IN (SELECT id FROM stale)`,
		},
		{
			name: "the name as the second member of the list",
			refused: `WITH one AS (SELECT 1 AS n), batch AS (SELECT id FROM probe WHERE id < 7)
UPDATE probe SET note = 'done' WHERE id IN (SELECT id FROM batch)`,
			corrected: `WITH one AS (SELECT 1 AS n), stale AS (SELECT id FROM probe WHERE id < 7)
UPDATE probe SET note = 'done' WHERE id IN (SELECT id FROM batch) AND id NOT IN (SELECT id FROM stale)`,
		},
		{
			name: "the name written in other letters, which fold to the same one",
			refused: `WITH BATCH AS (SELECT id FROM probe WHERE id < 7)
UPDATE probe SET note = 'done' WHERE id IN (SELECT id FROM batch)`,
			corrected: `WITH Stale AS (SELECT id FROM probe WHERE id < 7)
UPDATE probe SET note = 'done' WHERE id IN (SELECT id FROM batch) AND id NOT IN (SELECT id FROM Stale)`,
		},
		{
			name: "the name carried by a recursive list",
			refused: `WITH RECURSIVE batch AS (SELECT id FROM probe WHERE id < 7)
UPDATE probe SET note = 'done' WHERE id IN (SELECT id FROM batch)`,
			corrected: `WITH RECURSIVE stale AS (SELECT id FROM probe WHERE id < 7)
UPDATE probe SET note = 'done' WHERE id IN (SELECT id FROM batch) AND id NOT IN (SELECT id FROM stale)`,
		},
		{
			name: "commentary between the name and its keyword",
			refused: `WITH batch -- the window, or somebody's own rows
AS (SELECT id FROM probe WHERE id < 7)
UPDATE probe SET note = 'done' WHERE id IN (SELECT id FROM batch)`,
			corrected: `WITH stale -- the window, or somebody's own rows
AS (SELECT id FROM probe WHERE id < 7)
UPDATE probe SET note = 'done' WHERE id IN (SELECT id FROM batch) AND id NOT IN (SELECT id FROM stale)`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := windowShapesFiles(tc.refused)
			err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "spellings", Files: files})
			if err == nil || !strings.Contains(err.Error(), "the window is the relation named batch") {
				t.Fatalf("a body that binds the window's own name answered with %v; the drain refuses it by name, before it writes anything", err)
			}
			admin := dbtest.Open(t, migrateURL)
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
				t.Errorf("%d progress rows behind a file the window will never run: %v", n, err)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE note = 'done'"); n != 0 {
				t.Errorf("%d rows written by a run that refused the body", n)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'spellings' AND version = 2"); n != 0 {
				t.Errorf("%d history rows for a file that never ran", n)
			}

			files["000002_fill.up.sql"] = windowShapesData(tc.corrected)
			if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "spellings", Files: files}); err != nil {
				t.Fatalf("the same body with its own relation named otherwise: %v", err)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE note = 'done'"); n != 6 {
				t.Errorf("%d rows written by the corrected body, want the 6 its own list bounds off", n)
			}
		})
	}
}

// TestAWindowNameThatOnlyAValueOrAnInnerScopeOrAnotherNameHoldsStillDrains is the half that
// keeps the reading narrow. A quoted name of other letters is another relation; a CTE list
// inside a sub-expression shadows the window in the scope the server resolves it in (measured at
// the pinned version) rather than colliding with it; a longer name that merely contains the
// window's is a longer name; and `batch` as a column is a column. Every leg drains, and leaves
// nothing resumable behind.
func TestAWindowNameThatOnlyAValueOrAnInnerScopeOrAnotherNameHoldsStillDrains(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"a quoted name whose letters differ, which is another relation", `WITH "Batch" AS (SELECT id FROM probe WHERE id < 7)
UPDATE probe SET note = 'done' WHERE id IN (SELECT id FROM batch) AND id NOT IN (SELECT id FROM "Batch")`},
		{"a list of that name inside a sub-expression, which shadows rather than collides", `UPDATE probe SET note = 'done'
WHERE id IN (SELECT id FROM (WITH batch AS (SELECT 99 AS id) SELECT id FROM batch) inner_window)
   OR id IN (SELECT id FROM batch)`},
		{"a longer name that starts with the window's", `WITH batched AS (SELECT id FROM probe WHERE id < 7)
UPDATE probe SET note = 'done' WHERE id IN (SELECT id FROM batch) AND id IN (SELECT id FROM batched)`},
		{"a name that ends with the window's", `WITH upsert_batch AS (SELECT id FROM probe WHERE id < 7)
UPDATE probe SET note = 'done' WHERE id IN (SELECT id FROM batch) AND id IN (SELECT id FROM upsert_batch)`},
		{"batch as part of a column name the body reads", `WITH stale AS (SELECT batch_id AS id FROM upserts WHERE batch_id < 7)
UPDATE probe SET note = 'done' WHERE id IN (SELECT id FROM batch) AND id IN (SELECT id FROM stale)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{
				"000001_probe.up.sql": {Data: []byte(windowShapesProbe)},
				"000002_upserts.up.sql": {Data: []byte(`CREATE TABLE upserts (batch_id bigint PRIMARY KEY);
INSERT INTO upserts (batch_id) SELECT g FROM generate_series(1, 6) g`)},
				"000003_fill.up.sql": windowShapesData(tc.body),
			}
			if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "narrow", Files: files}); err != nil {
				t.Fatalf("the window refused a body that binds nothing of its own: %v", err)
			}
			admin := dbtest.Open(t, migrateURL)
			if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE note = 'done'"); n == 0 {
				t.Error("no row carries the value: the body ran over no window at all")
			}
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
				t.Errorf("%d progress rows left by a drain that finished", n)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'narrow' AND version = 3"); n != 1 {
				t.Errorf("%d history rows for the drained file, want 1", n)
			}
		})
	}
}

// TestADataBodyThatWritesTheWindowKeyIsRefusedBeforeTheProgressRow is the third reading's
// refusal at the door an operator meets it at: the sentence names the table and the column the
// cursor runs over, nothing of the file is written, and the file the author meant — the same
// backfill of the columns the key *holds* — drains behind it. The owner has history, so this is
// `Migrate` refusing rather than a fresh installation draining its own rows.
func TestADataBodyThatWritesTheWindowKeyIsRefusedBeforeTheProgressRow(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	files := windowShapesFiles(`UPDATE probe SET id = id + 1000, note = 'rekeyed' WHERE id IN (SELECT id FROM batch)`)
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "keywrite", Files: fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte(windowShapesProbe)},
	}}); err != nil {
		t.Fatal(err)
	}
	err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "keywrite", Files: files})
	if err == nil || !strings.Contains(err.Error(), "probe's key id") {
		t.Fatalf("a body that writes the key the window runs over answered with %v", err)
	}
	admin := dbtest.Open(t, migrateURL)
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
		t.Errorf("%d progress rows behind a drain that never started: %v", n, err)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE id > 12"); n != 0 {
		t.Errorf("%d rows carry a key the refused body moved", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'keywrite'"); n != 1 {
		t.Errorf("%d history rows, want only the owner's own layout file", n)
	}

	files["000002_fill.up.sql"] = windowShapesData(`UPDATE probe SET note = 'filled' WHERE id IN (SELECT id FROM batch)`)
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "keywrite", Files: files}); err != nil {
		t.Fatalf("the corrected file: %v", err)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE note = 'filled'"); n != 12 {
		t.Errorf("%d of 12 rows drained", n)
	}
}

// TestOnlyTheWindowKeyIsRefusedAmongWhatABodyWrites is that reading's boundary from the other
// side. A key mentioned in the predicate, as the source of another column's value, in the words
// of a value, or as another table's key of the same name is a key the cursor holds still, and
// those files drain. What must refuse is the same column written by the UPDATE over the drained
// table, in each of the shapes PostgreSQL takes for writing a column — the second item of the
// list, a multi-column assignment, an aliased target with or without its `AS`, the target written
// as ONLY or with its star, and a name written quoted — and the rows an INSERT or a MERGE adds to
// that same table, which move the key set without assigning to one key at all.
func TestOnlyTheWindowKeyIsRefusedAmongWhatABodyWrites(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		writes     bool
	}{
		{
			name: "the key in the predicate, which is the window working",
			body: `UPDATE probe SET note = 'marked' WHERE id IN (SELECT id FROM batch)`,
		},
		{
			name: "the key as the source of another column's value",
			body: `UPDATE probe SET note = 'row ' || id WHERE id IN (SELECT id FROM batch)`,
		},
		{
			name: "the words of an assignment inside the value being written",
			body: `UPDATE probe SET note = 'set id = 5 would be a re-key' WHERE id IN (SELECT id FROM batch)`,
		},
		{
			name: "another table's key of the same name, moved by its own statement",
			body: `UPDATE other SET id = id + 1000 WHERE other_id IN (SELECT id FROM batch)`,
		},
		{
			name: "a second item of the list that is not the key",
			body: `UPDATE probe SET note = 'x', passes = 1 WHERE id IN (SELECT id FROM batch)`,
		},
		{
			name:   "the key as the second item of the list",
			body:   `UPDATE probe SET note = 'x', id = id + 1 WHERE id IN (SELECT id FROM batch)`,
			writes: true,
		},
		{
			name:   "the key inside a multi-column assignment",
			body:   `UPDATE probe SET (id, note) = (id + 1, 'x') WHERE id IN (SELECT id FROM batch)`,
			writes: true,
		},
		{
			name:   "the drained table named through an alias",
			body:   `UPDATE probe AS p SET id = id + 1 WHERE p.id IN (SELECT id FROM batch)`,
			writes: true,
		},
		{
			// The same alias without the word PostgreSQL does not require: one word away
			// from the leg above, and the re-key the fourteenth round met.
			name:   "the alias written without its AS",
			body:   `UPDATE probe p SET id = id + 1 WHERE p.id IN (SELECT id FROM batch)`,
			writes: true,
		},
		{
			name:   "the target written as ONLY, with the parenthesis it may carry",
			body:   `UPDATE ONLY (probe) SET id = id + 1 WHERE id IN (SELECT id FROM batch)`,
			writes: true,
		},
		{
			name:   "the target written with the star PostgreSQL allows after its name",
			body:   `UPDATE probe * SET id = id + 1 WHERE id IN (SELECT id FROM batch)`,
			writes: true,
		},
		{
			name:   "the key written quoted, the only spelling a reserved word needs",
			body:   `UPDATE probe SET "id" = id + 1 WHERE id IN (SELECT id FROM batch)`,
			writes: true,
		},
		{
			// Not an UPDATE at all: the upsert's arm names no target, and the statement
			// carrying it names the drained table.
			name:   "the key moved by an upsert's own arm",
			body:   `INSERT INTO probe (id, note) SELECT id, 'x' FROM batch ON CONFLICT (id) DO UPDATE SET id = probe.id + 1000`,
			writes: true,
		},
		{
			name:   "the key moved by a merge arm",
			body:   `MERGE INTO probe p USING batch ON p.id = batch.id WHEN MATCHED THEN UPDATE SET id = p.id + 1000`,
			writes: true,
		},
		{
			// The same append, aimed at a table whose key is nobody's cursor here.
			name: "another table's rows added by this body",
			body: `INSERT INTO other (id, other_id) SELECT id + 100, id FROM batch`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := windowShapesAndOtherFiles(tc.body)
			err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "targets", Files: files})
			admin := dbtest.Open(t, migrateURL)
			if tc.writes {
				if err == nil || !strings.Contains(err.Error(), "probe's key id") {
					t.Fatalf("a body that writes the window's key answered with %v", err)
				}
				if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
					t.Errorf("%d progress rows behind the refusal: %v", n, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("the window refused a body that leaves the key where it is: %v", err)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
				t.Errorf("%d progress rows left by a drain that finished", n)
			}
		})
	}
}

// TestWhatABodyTheWindowNeverWrapsHasNoKeyReadingAskedOfIt keeps the two doors from being read as
// one. Both readings are conditions of the wrapper: `bindsWindowRelation` is asked where the
// executor is about to merge its own CTE into the body's list, and `movesTheWindowKey` where a
// cursor will walk the rows the body touched. A body excepted as bounding itself gets neither — no
// window is wrapped around it, and the drain runs it once as written — so the same key write this
// file refuses one marker earlier runs here, which is what the exception says: the author claimed
// the bound, and one statement over the table is that claim.
func TestWhatABodyTheWindowNeverWrapsHasNoKeyReadingAskedOfIt(t *testing.T) {
	url, _ := dbtest.URLs(t)
	err := db.Migrate(t.Context(), url, db.MigrationSource{Owner: "unwindowed", Files: fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte(windowShapesProbe)},
		"000002_fill.up.sql": {Data: []byte("-- pkit: phase=data\n-- pkit: batch=5\n-- pkit: table=probe\n" +
			"-- pkit: allow=data-body-unbounded reason=one statement over a table this release owns\n" +
			"UPDATE probe SET id = id + 1000, note = 'rekeyed'")},
	}})
	if err != nil {
		t.Fatalf("a body no window wraps: %v", err)
	}
	admin := dbtest.Open(t, url)
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE id > 12"); n != 12 {
		t.Errorf("%d of 12 rows re-keyed by the statement as written; this file runs no window", n)
	}
}

// windowShapesAndOtherFiles adds a second table to the probe: keyed by a column of the same
// name, and the target of one leg's own statement. The window drains `probe`, and what another
// table's key does under a body aimed at it is nobody's cursor.
func windowShapesAndOtherFiles(body string) fstest.MapFS {
	return fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte(windowShapesProbe)},
		"000002_other.up.sql": {Data: []byte(`CREATE TABLE other (id bigint PRIMARY KEY, other_id bigint REFERENCES probe (id));
INSERT INTO other (id, other_id) SELECT g, g FROM generate_series(1, 12) g`)},
		"000003_fill.up.sql": windowShapesData(body),
	}
}

// TestADataBodyThatEmptiesTheTableItDrainsStillDrains is the reading's other edge. What the cursor
// cannot survive is a key set that grows above it; a body that takes rows away moves that set the
// one safe way, and a purge written as a data file is a purge and not a re-key. Nothing here
// refuses it, so this drains to its end and applies its version.
func TestADataBodyThatEmptiesTheTableItDrainsStillDrains(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	files := windowShapesFiles(`DELETE FROM probe WHERE id IN (SELECT id FROM batch)`)
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "purge", Files: files}); err != nil {
		t.Fatalf("the window refused a body that only takes rows away: %v", err)
	}
	admin := dbtest.Open(t, migrateURL)
	if n := countRows(t, admin, "SELECT count(*) FROM probe"); n != 0 {
		t.Errorf("%d rows survive a purge of the table the drain windows over", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
		t.Errorf("%d progress rows left by a drain that finished", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'purge' AND version = 2"); n != 1 {
		t.Errorf("%d history rows for the drained purge, want 1", n)
	}
}

// TestTheWorkersDrainEndsAtTheBoundATickGivesItself asks what stops a drain that runs forever for
// a reason the key reading cannot see. It used to be an append to the drained table, which the
// fourteenth round showed is the same harm as writing the key and is now refused; what is left is
// the append the body never names — rows put into the table through a view over it, which the
// server routes wherever the view says. The cursor is right that nothing above it was written yet,
// and the answer has to be a number, because the alternative is a tick that repeats work and never
// applies the version.
//
// The bound is reached, so the case costs a tick's worth of windows; its own context is the
// deadline that turns a regression to no bound at all into a failure rather than a hang.
func TestTheWorkersDrainEndsAtTheBoundATickGivesItself(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	// The probe, and a plain view over it. The append below runs through the view, which is the
	// spelling of "this table gains rows" that names something other than the drained table.
	seed := &fstest.MapFile{Data: []byte(windowShapesProbe + `;
CREATE VIEW probe_all AS SELECT id, note FROM probe`)}
	files := fstest.MapFS{
		"000001_probe.up.sql": seed,
		"000002_fill.up.sql": windowShapesData(`INSERT INTO probe_all (id, note)
SELECT (SELECT max(id) FROM probe) + row_number() OVER (), 'grown' FROM batch`),
	}
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "ticks", Files: fstest.MapFS{
		"000001_probe.up.sql": seed,
	}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	err := db.Backfill(ctx, migrateURL, db.MigrationSource{Owner: "ticks", Files: files})
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("the worker's tick did not end on its own: this drain has no bound, and every tick rewrites work while holding the job's advisory lock")
	}
	if !errors.Is(err, db.ErrBackfillBudget) {
		t.Fatalf("the bound a tick gives itself reported %v, not ErrBackfillBudget; a run that may not be open forever has to say so", err)
	}
	if !strings.Contains(err.Error(), "10000 batches of 5") {
		t.Errorf("the report does not name the bound it stopped at: %v", err)
	}
	admin := dbtest.Open(t, migrateURL)
	// A bound is a stop and not a refusal: what committed stands, the cursor says where the next
	// tick starts, and the version is not applied over work that is still there.
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 1 {
		t.Errorf("%d progress rows: a bounded tick leaves the drain resumable, which is what the next tick reads", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'ticks' AND version = 2"); n != 0 {
		t.Errorf("%d history rows for a version whose drain stopped short", n)
	}
}
