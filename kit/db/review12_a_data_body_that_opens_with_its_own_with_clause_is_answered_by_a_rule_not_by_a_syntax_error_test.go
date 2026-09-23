package db_test

// review12_a_data_body_that_opens_with_its_own_with_clause_is_answered_by_a_rule_and_not_by_a_
// syntax_error_test.go is the twelfth round's case for the wrapper and for the one promise the
// runner makes about it.
//
// `windowedBody` writes the window and pastes the body after it:
//
//	"WITH batch AS (SELECT … ORDER BY … LIMIT n)\n" + strings.TrimRight(m.body, " \n\t;")
//
// PostgreSQL takes one `WITH` per statement, so a body that opens with one — the idiomatic
// shape for a backfill that names the rows it is about to touch — comes out as two, and the
// server answers the file the kernel assembled, not the file its author wrote. Measured at
// HEAD over a twelve-row table with `batch=5`:
//
//	WITH stale AS (SELECT id FROM probe WHERE note = '')
//	UPDATE probe SET note = 'done' WHERE id IN (SELECT id FROM stale INTERSECT SELECT id FROM batch)
//	→ db: migrate: shape/000002_fill.up.sql: ERROR: syntax error at or near "WITH" (SQLSTATE 42601)
//	  with 1 row left in schema_migration_backfill
//
// Four spellings beside it — `UPDATE … FROM batch b`, `INSERT … SELECT id FROM batch`, a body
// ending in a comment, `DELETE … IN (SELECT id FROM batch)` — apply and drain to the end, so
// this is the wrapper's, not the body's.
//
// The harm is what is left behind. `kit/db/README.md` promises:
//
//	"A refusal of a data file leaves nothing resumable: the progress row means 'this drain
//	 started, resume it', so a shape the window cannot run is refused before that row exists."
//
// This is exactly a shape the window cannot run, and the row is there: `drain` refuses the
// *statement count* before `beginDrain` and nothing else, so the file is now "in flight" —
// `planOwner` resumes a drain it finds started whatever is queued behind it, every later
// `Migrate` and every `schema-backfill` tick re-runs the same failing statement, and the
// owner never converges. The message carries no rule id and no remedy, so the operator has
// no line to fix: the file is correct SQL, and the sentence says so.
//
// Either answer passes. The wrapper can carry the body's own `WITH` (append `batch` to its
// list, or hand the window as the subquery the body already selects from), and the file then
// drains like the four beside it. Or the shape can be refused the way `a data file is one
// statement` is refused — named, and before `beginDrain` — which leaves the author a file to
// rewrite and the installation nothing to retry. What may not stand is the third answer it
// gives today: PostgreSQL's syntax error, and a drain forever half-started.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestADataBodyThatOpensWithItsOwnWithClauseIsAnsweredByARuleNotByASyntaxError(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	files := fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte(`CREATE TABLE probe (id bigint PRIMARY KEY, note text NOT NULL DEFAULT '');
INSERT INTO probe (id) SELECT g FROM generate_series(1, 12) g`)},
		"000002_fill.up.sql": {Data: []byte(`-- pkit: phase=data
-- pkit: batch=5
-- pkit: table=probe
WITH stale AS (SELECT id FROM probe WHERE note = '')
UPDATE probe SET note = 'done' WHERE id IN (SELECT id FROM stale INTERSECT SELECT id FROM batch)`)},
	}
	err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "ctefill", Files: files})
	admin := dbtest.Open(t, migrateURL)
	if err == nil {
		if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE note = 'done'"); n != 12 {
			t.Errorf("%d of 12 rows drained: the file applied without its window", n)
		}
		if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'ctefill' AND version = 2"); n != 1 {
			t.Errorf("%d history rows for a file that applied", n)
		}
		return
	}
	// A refusal the author can act on names itself — but it has to leave nothing resumable,
	// which is the promise the README makes for exactly this case.
	if !strings.Contains(err.Error(), "refusal ") && !strings.Contains(err.Error(), "rule ") {
		t.Errorf("the window wrapped a body that opens with its own WITH into two of them and answered with the server's own error, which names no rule and offers no remedy: %v", err)
	}
	// Counted through the catalogue first: a refusal moved into the guard would answer this
	// run before the runner ever created its own two tables, and a case that queries a
	// relation the behaviour under test may never create reports its own absence, not the
	// behaviour's.
	if countRows(t, admin, "SELECT count(*) FROM pg_class WHERE relname = 'schema_migration_backfill' AND relnamespace = current_schema()::regnamespace") > 0 {
		if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
			t.Errorf("%d progress rows behind a drain the window can never run: every later run will take it up again and fail the same way", n)
		}
	}
}
