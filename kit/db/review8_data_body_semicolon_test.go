package db_test

// review8_data_body_semicolon_test.go is the ninth review's case for the executor's
// refusal of a phase=data file: "a data file is one statement: the window wraps the
// body, and a second statement would be run over a window of its own with no cursor
// between them; split the file" (kit/db/backfill.go, drain).
//
// The refusal is right about the shape it was written for — `review3_data_file_shape_test.go`
// shows a body of two bounded UPDATEs, which no rule refuses and which the window cannot
// run. What that case leaves open is the file the sentence is *wrong* about: a body that is
// one statement to PostgreSQL because the semicolon sits inside a dollar-quoted value it is
// writing. `splitStatements` reads a function body from the inside — the documented
// approximation, whose false positive an `allow=` exists to except — and this refusal asks
// that same split the question an `allow=` can reach no answer to: it is not a rule, so no
// marker excepts it, and the remedy it names ("split the file") cannot produce a
// single-statement file out of a file that already has one. The value's punctuation is the
// only thing that moved the count.
//
// Two consequences, both asserted below. The file is refused by every door: `Migrate` for a
// fresh owner, and `db.Backfill` for the worker's, because both call `drain`. And the same
// value written the other way PostgreSQL allows — an ordinary `'…'` literal, which the
// splitter reads as one token — is applied by the same run. Same statement, same rows,
// different quote spelling, different answer from the runner.
//
// Nothing here asserts anything about the refusal's *message*: the legs read the table and
// the ledger, which are what the fixed behaviour writes, and the control leg passes today,
// so the fault is the letters of the quote and not the question.

import (
	"fmt"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// oneStatementSeed: twenty-five rows so a batch of ten takes three windows, and a note
// column to hold the value the body writes.
const oneStatementSeed = `CREATE TABLE probe (id bigint PRIMARY KEY, note text);
INSERT INTO probe (id) SELECT g FROM generate_series(1, 25) g`

// oneStatementBodies: the same UPDATE, the same value — `a; b` — written two ways. The
// dollar spelling is the one that carries no escaping obligation and is therefore how a
// backfill writes JSON, prose or anything else with an apostrophe in it.
var oneStatementBodies = map[string]string{
	"an ordinary literal, the control": `-- pkit: phase=data
-- pkit: batch=10
-- pkit: table=probe
UPDATE probe SET note = 'a; b' WHERE id IN (SELECT id FROM batch)`,
	// The value is spelled identically in both: dollar quoting takes the text literally,
	// so the delimiters sit against it with no space of their own.
	"the same value dollar-quoted": `-- pkit: phase=data
-- pkit: batch=10
-- pkit: table=probe
UPDATE probe SET note = $n$a; b$n$ WHERE id IN (SELECT id FROM batch)`,
}

func TestADataBodyIsOneStatementWhenItsValueCarriesTheSemicolon(t *testing.T) {
	for _, spelling := range []string{"an ordinary literal, the control", "the same value dollar-quoted"} {
		t.Run(spelling, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{
				"000001_probe.up.sql": {Data: []byte(oneStatementSeed)},
				"000002_note.up.sql":  {Data: []byte(oneStatementBodies[spelling])},
			}
			if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "body", Files: files}); err != nil {
				t.Errorf("the run that must drain the file refused it: %v", err)
			}
			admin := dbtest.Open(t, migrateURL)
			// The table is the assertion: a drain that ran wrote the value into every
			// row, in all three windows.
			if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE note = 'a; b'"); n != 25 {
				t.Errorf("%d of 25 rows carry the value the body writes: the batched drain did not run this file to its end", n)
			}
			// And the two tables hold the one fact they are supposed to: applied, with
			// nothing left to resume.
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'body' AND version = 2"); n != 1 {
				t.Errorf("%d history rows for the data file, want 1: a drain that never ran leaves the version unapplied", n)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
				t.Errorf("%d rows left in schema_migration_backfill: a finished drain leaves none", n)
			}
			// The worker's door is the second door to the same file, and it asks the same
			// question of the same split (kit/db/backfill.go, BackfillWith → drain). A file
			// no door can apply has no remedy at all, whatever the message names.
			if err := db.Backfill(t.Context(), migrateURL, db.MigrationSource{Owner: "body", Files: files}); err != nil {
				t.Errorf("the worker's door refused the file too: %v", err)
			}
		})
	}
}

// TestTheWindowedFormOfThatBodyIsOneStatementToPostgreSQL is the premise of the case
// above, read off the server rather than off the runner. It builds the wrapped text the
// way kit/db/README.md and backfill.go document it — the window in front, the owner's body
// behind it, no trailing semicolon — and asks PostgreSQL to plan it. The inference is
// specific to this string: were the semicolon inside `$n$ … $n$` a statement boundary, the
// second piece would be `b $n$ … WHERE id IN (SELECT id FROM batch)`, which is not SQL, and
// the whole submission would answer with a syntax error. That it plans is the server saying
// the file is one statement, which is the fact the executor's refusal gets wrong.
func TestTheWindowedFormOfThatBodyIsOneStatementToPostgreSQL(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	admin := dbtest.Open(t, migrateURL)
	if _, err := admin.ExecContext(t.Context(), oneStatementSeed); err != nil {
		t.Fatalf("the fixture: %v", err)
	}
	wrapped := fmt.Sprintf("WITH batch AS (SELECT id FROM probe WHERE id > 0 ORDER BY id LIMIT 10)\n%s",
		"UPDATE probe SET note = $n$a; b$n$ WHERE id IN (SELECT id FROM batch)")
	if _, err := admin.ExecContext(t.Context(), wrapped); err != nil {
		t.Fatalf("the server would not run the windowed body as one statement (%v), which is the one thing the refusal claims about it", err)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE note = 'a; b'"); n != 10 {
		t.Errorf("%d rows carry the value, want the 10 of one window: the statement did not run as the window the drain would run", n)
	}
}
