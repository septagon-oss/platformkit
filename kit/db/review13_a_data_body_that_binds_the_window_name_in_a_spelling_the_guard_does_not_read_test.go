package db_test

// review13_a_data_body_that_binds_the_window_name_in_a_spelling_the_guard_does_not_read_test.go
// is the thirteenth round's case for the refusal the twelfth round added.
//
// The twelfth round's finding 4 was a `phase=data` body that opens with its own `WITH`
// list: the wrapper pasted a second `WITH` in front of it, the server's own syntax error
// arrived after `beginDrain` had written the progress row, and the drain was left forever
// half-started. The cure joins the two CTE lists and refuses, before the progress row, the
// one merged statement still cannot run — a body that binds the window's own name `batch`.
//
// The refusal asks `shape` for `\bbatch\s+as\s*\(`. Two spellings of the same binding answer
// that question no, and both reach the server with two CTEs of one name:
//
//	WITH "batch" AS (…)    -- a quoted lower-case name is the same identifier to PostgreSQL
//	WITH batch (id) AS (…) -- a CTE column list sits between the name and `as`
//
// while `reBatchWindow`, the question that decides whether the body is wrapped at all, does
// read the body's `FROM batch` in both files. So the file is wrapped, the merged list carries
// the window and the body's own relation under one name, and what the operator reads is the
// server's text with a progress row behind it — the exact state the refusal exists to leave
// uncreated, in a spelling the guard does not read.
//
// Every assertion is on state the fixed behaviour produces and the defect does not: which rows
// the drain wrote, and whether a progress row exists. The corrected file is then drained,
// which is the passing branch of the whole case.

import (
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

const review13Probe = `CREATE TABLE probe (id bigint PRIMARY KEY, note text NOT NULL DEFAULT '');
INSERT INTO probe (id) SELECT g FROM generate_series(1, 12) g`

func TestADataBodyThatBindsTheWindowNameInASpellingTheGuardDoesNotReadIsRefusedBeforeTheDrainStarts(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{
			name: "the window's name written quoted, which is the same identifier",
			body: `WITH "batch" AS (SELECT id FROM probe WHERE id < 7)
UPDATE probe SET note = 'done' WHERE id IN (SELECT id FROM batch)`,
		},
		{
			name: "the window's name written with its own column list",
			body: `WITH batch (id) AS (SELECT id FROM probe WHERE id < 7)
UPDATE probe SET note = 'done' WHERE id IN (SELECT id FROM batch)`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{
				"000001_probe.up.sql": {Data: []byte(review13Probe)},
				"000002_fill.up.sql":  {Data: []byte("-- pkit: phase=data\n-- pkit: batch=5\n-- pkit: table=probe\n" + tc.body)},
			}
			err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "quoted", Files: files})
			if err == nil {
				t.Fatalf("a data file that binds the window's own name applied; its rows would be its own and the cursor would advance over a window nobody read")
			}
			admin := dbtest.Open(t, migrateURL)
			// The state the refusal promises and the server's error does not leave: a run that
			// got as far as PostgreSQL has already written the row every later run reads as
			// "this drain started, resume it".
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
				t.Errorf("%d progress rows behind a file the window will never run: the drain refuses it before it starts, so a file that never ran once is not a drain to resume: %v", n, err)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE note = 'done'"); n != 0 {
				t.Errorf("%d rows were written by a run that refused the file", n)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'quoted' AND version = 2"); n != 0 {
				t.Errorf("%d history rows for a file that never ran", n)
			}

			// The passing branch: the same file with the window's name left to the kernel is the
			// file the author meant, and it drains over its windows.
			files["000002_fill.up.sql"] = &fstest.MapFile{Data: []byte(`-- pkit: phase=data
-- pkit: batch=5
-- pkit: table=probe
WITH stale (id) AS (SELECT id FROM probe WHERE id < 7)
UPDATE probe SET note = 'done' WHERE id IN (SELECT id FROM batch) AND id IN (SELECT id FROM stale)`)}
			if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "quoted", Files: files}); err != nil {
				t.Fatalf("the corrected file: %v", err)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE note = 'done'"); n != 6 {
				t.Errorf("%d of the 6 rows the corrected body bounds drained", n)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'quoted' AND version = 2"); n != 1 {
				t.Errorf("%d history rows for the drained file, want 1", n)
			}
		})
	}
}

// TestTheDrainRefusesOnlyWhatTheServerWouldRefuse pins the sentence the round's rule reads:
// a body that names the window in prose the server still runs is not refused, so the guard
// that refuses a binding cannot be the reason a correct file is refused. `batch` in a value
// binds nothing, and a body that reads the window under its own quoted alias of some *other*
// relation is a body the merged list runs.
func TestABodyThatNamesTheWindowOnlyInAValueStillDrainsUnderTheGuard(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	files := fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte(review13Probe)},
		"000002_fill.up.sql": {Data: []byte(`-- pkit: phase=data
-- pkit: batch=5
-- pkit: table=probe
WITH stale (id) AS (SELECT id FROM probe WHERE id < 7)
UPDATE probe SET note = 'batch AS (the word in a value)' WHERE id IN (SELECT id FROM batch)`)},
	}
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "prose", Files: files}); err != nil {
		t.Fatalf("a body whose only `batch AS (` is the value it writes: %v", err)
	}
	admin := dbtest.Open(t, migrateURL)
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE note LIKE 'batch AS (%'"); n != 12 {
		t.Errorf("%d of 12 rows carry the value: the window guard read a literal as a binding", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
		t.Errorf("%d progress rows left by a drain that finished", n)
	}
}
