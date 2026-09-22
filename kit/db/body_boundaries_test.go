package db_test

// body_boundaries_test.go is the delivery's own case for the one place the guard and
// the drain can lose their place in a body: where a value or a comment *ends*.
//
// migrations/README.md says the decision whether to wrap a body is made from one
// reading of one text, and that the reading puts away "the contents of every string
// literal" — so a body whose only window reference sits inside a value it is writing is
// not wrapped, and one that reads the window past an apostrophe in commentary is not
// refused. Both claims are about PostgreSQL's idea of where a token ends, and the
// spellings that decide it are more than `'…'` and `--`: a block comment nests and runs
// over lines, a `$tag$ … $tag$` body is one value whatever it carries inside it, and an
// `E'…'` takes a backslash. A reading that lost its place at any of the three answered
// the window question wrongly in both directions at once:
//
//   - text that is data or commentary read as SQL, so a body that never reads the
//     window was wrapped, and its one whole-table statement ran once per window,
//     writing every row as many times as the table has windows;
//   - SQL that came after an apostrophe inside one of them read as gone, so a bounded
//     body was refused as unbounded — and the remedy its refusal offers
//     (`allow=data-body-unbounded`) then ran that same body with no window at all.
//
// `passes` counts the transactions that wrote a row, so which way the guard got it is
// read off the table rather than off the message it printed.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// boundaryTable is the same 25 rows the other body cases drain, with a text column to
// write a value into and `passes` to count the transactions that touched each row.
const boundaryTable = `CREATE TABLE probe (id bigint PRIMARY KEY, a text, passes integer NOT NULL DEFAULT 0);
INSERT INTO probe (id) SELECT g FROM generate_series(1,25) g`

func TestABodyIsBoundedByWhatPostgresReadsAsOneValueOrOneComment(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		// readsWindow is what PostgreSQL runs: whether the SQL, and not the data beside
		// it, names the drain's window relation.
		readsWindow bool
	}{
		{
			name:        "a window phrase inside a block comment nested in a block comment",
			body:        "/* backfill /* one window at a time */ from batch */\nUPDATE probe SET passes = passes + 1 WHERE id > 0",
			readsWindow: false,
		},
		{
			name:        "a window phrase inside an untagged dollar-quoted value",
			body:        "UPDATE probe SET passes = passes + 1, a = $$WHERE id IN (SELECT id FROM batch)$$ WHERE id > 0",
			readsWindow: false,
		},
		{
			name:        "a window phrase inside an E-string, past its escaped apostrophe",
			body:        "UPDATE probe SET passes = passes + 1, a = E'a\\'b from batch' WHERE id > 0",
			readsWindow: false,
		},
		{
			name:        "the window read past a dollar-quoted value carrying an apostrophe and two dashes",
			body:        "UPDATE probe SET passes = passes + 1, a = $tag$it's -- just a value$tag$ WHERE id IN (SELECT id FROM batch)",
			readsWindow: true,
		},
		{
			name:        "the window read past a nested block comment carrying an apostrophe",
			body:        "/* one /* like this */ and it's still commentary */\nUPDATE probe SET passes = passes + 1 WHERE id IN (SELECT id FROM batch)",
			readsWindow: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{"000001_probe.up.sql": {Data: []byte(boundaryTable)}}
			if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "boundary", Files: files}); err != nil {
				t.Fatalf("the owner's schema file: %v", err)
			}
			files["000002_backfill.up.sql"] = &fstest.MapFile{Data: []byte("-- pkit: phase=data\n-- pkit: batch=10\n-- pkit: table=probe\n" + tc.body)}
			err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "boundary", Files: files})
			admin := dbtest.Open(t, migrateURL)
			switch {
			case tc.readsWindow && err != nil:
				t.Errorf("a body whose SQL reads the window was refused: %v\nthe reading that refused it lost its place in a value or a comment above the line that reads the window; the remedy the refusal offers (allow=data-body-unbounded) then runs the same body with no window around it", err)
			case !tc.readsWindow && err != nil && !strings.Contains(err.Error(), "rule data-body-unbounded"):
				t.Errorf("the file failed for something other than the rule refusal it earns: %v", err)
			}
			// The rows answer whichever honest way the guard took. Nothing was written
			// when the file was refused; one transaction per row's window when it ran.
			// What is never honest is the table visited once per window.
			if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE passes > 1"); n != 0 {
				t.Errorf("%d of 25 rows were written more than once (max %d): the body's window reference is %s, and it was wrapped anyway",
					n, countRows(t, admin, "SELECT coalesce(max(passes),0) FROM probe"), map[bool]string{true: "its own SQL", false: "inside a value or a comment"}[tc.readsWindow])
			}
			if tc.readsWindow {
				if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE passes <> 1"); n != 0 {
					t.Errorf("%d of 25 rows were not written exactly once: the drain did not run this body over its windows", n)
				}
			} else if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE passes <> 0"); n != 0 {
				t.Errorf("%d of 25 rows were written by a file the guard should have refused before it connected", n)
			}
		})
	}
}

// TestTheOutboxRuleStillReadsTheLiteralsTheWindowQuestionPutsAway: the two readings of
// the body differ on purpose, and the difference is the outbox's. `data-writes-outbox`
// asks its question of the text with the literals left whole — an over-approximation
// that refuses a backfill which only *says* the outbox's name, on the grounds that the
// sentence is not worth the risk it hides. Learning where a dollar-quoted value ends
// belongs to the reading that answers the window question; it must not take the other
// reading's text away with it.
func TestTheOutboxRuleStillReadsTheLiteralsTheWindowQuestionPutsAway(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	files := fstest.MapFS{"000001_probe.up.sql": {Data: []byte(boundaryTable)}}
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "outboxread", Files: files}); err != nil {
		t.Fatalf("the owner's schema file: %v", err)
	}
	files["000002_backfill.up.sql"] = &fstest.MapFile{Data: []byte(`-- pkit: phase=data
-- pkit: batch=10
-- pkit: table=probe
UPDATE probe SET passes = passes + 1, a = $$INSERT INTO platformkit_outbox (name) SELECT 'x' FROM batch$$ WHERE id IN (SELECT id FROM batch)`)}
	err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "outboxread", Files: files})
	if err == nil || !strings.Contains(err.Error(), "data-writes-outbox") {
		t.Errorf("a data file naming the outbox inside a dollar-quoted value answered %v; the outbox rule reads literals intact on purpose, and the window question reading them away would switch it off", err)
	}
}
