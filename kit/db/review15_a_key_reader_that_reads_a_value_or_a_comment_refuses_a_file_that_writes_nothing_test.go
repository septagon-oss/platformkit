package db_test

// review15_a_key_reader_that_reads_a_value_or_a_comment_is_no_write_to_it_test.go asks the
// window's key reading the question both documents say it asks: which table does a *statement*
// of this body write.
//
// `migrations/README.md`, under the rule table, describes the executor's third refusal:
//
//	"a body that only *reads* the key, in a predicate or as another column's value, **or in the
//	words of a value it stores**, is no write to it at all."
//
// and the same page says of the rules above it that they read "text with comments stripped" and
// that "two dashes inside a literal is data, not the comment that would otherwise hide the rest
// of its line from the guard". `kit/db/backfill.go` asks the two construct readings — the CTE
// list and the key set — of `m.body`, which is the file's own words with its comments intact and
// its literals unput-away (the fields beside it say so: `plain` is `body` with the comments gone
// and the case folded, `shape` is `plain` with the contents of every literal put away, and it is
// `shape` the structural question "does this body read the window" is asked of).
//
// So the reading that has no `allow=` to answer it — the one the guide calls out as the refusal
// an author cannot contest, and whose named remedy is to take the window off the body altogether
// — is asked of the one text in the package that keeps both a comment and a value. Three bodies
// that write nothing but the column a window holds are refused by it: one whose value spells an
// `UPDATE` of the key, one whose value spells an `INSERT` into the drained table, and one whose
// own comment names the harm the file exists to avoid. The first two are the case the guide
// itself states; the third is the file that explains itself to a reviewer, which is the shape
// every migration in this repository is written in.
//
// Each leg asserts the fixed behaviour, not the refusal: the body drains, so the rows carry the
// value the body wrote, the ledger holds the version, and no resumable row stands behind a run
// that finished. A leg that is refused fails on all three counts whatever sentence it prints, so
// the case does not read the refusal's wording to find its answer.

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestAKeyReaderThatReadsAValueOrACommentRefusesAFileThatWritesNothing(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{
			name: "a value whose words spell an UPDATE of the key the cursor runs over",
			body: `UPDATE probe SET note = 'UPDATE probe SET id = id + 100000 WHERE true'
 WHERE id IN (SELECT id FROM batch)`,
		},
		{
			name: "a value whose words spell an INSERT into the table the window drains",
			body: `UPDATE probe SET note = 'INSERT INTO probe (id) VALUES (99)'
 WHERE id IN (SELECT id FROM batch)`,
		},
		{
			name: "a comment that names the re-key this file does not perform",
			body: `-- Unlike a re-key, this file never writes the key: it would take an
-- UPDATE probe SET id = id + 1, or an INSERT INTO probe of the row it moved,
-- and either would leave the cursor chasing its own writes.
UPDATE probe SET note = 'filled' WHERE id IN (SELECT id FROM batch)`,
		},
		{
			name: "a comment that spells the whole body it is not part of",
			body: `-- The refused shape, kept here so a reviewer sees why the file is written this way:
--   INSERT INTO probe (id) SELECT id + 1 FROM probe;
UPDATE probe SET note = 'filled' WHERE id IN (SELECT id FROM batch)`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := windowShapesFiles(tc.body)
			if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "words", Files: files}); err != nil {
				t.Fatalf("the window refused a body that writes nothing but the column a window holds: %v", err)
			}
			admin := dbtest.Open(t, migrateURL)
			// The body ran, over a window: the rows carry what it wrote.
			if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE note <> ''"); n == 0 {
				t.Error("no row carries the value the body writes: the file applied without draining")
			}
			// Nothing resumable stands behind a finished drain, and the version applied.
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
				t.Errorf("%d progress rows left by a drain that finished", n)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'words' AND version = 2"); n != 1 {
				t.Errorf("%d history rows for the drained file, want 1: a refusal wrote the file nowhere", n)
			}
			// The harm the refusal exists for: no key moved, and no row added.
			if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE id > 100000"); n != 0 {
				t.Errorf("%d rows carry a key above the table's own range", n)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM probe"); n != 12 {
				t.Errorf("%d rows in the drained table, want the 12 its owner created", n)
			}
		})
	}
}
