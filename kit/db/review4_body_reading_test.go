package db_test

// review4_body_reading_test.go is the fourth review's case for the one place the
// kernel's two readings of a file's body can disagree with the body itself.
//
// kit/db/migration_header.go states why the two readings were made one:
//
//	A second reading of a second text is how a file gets judged for one thing and
//	executed as another.
//
// and the kernel does run one text — `plain`, "the body with the comments gone and
// the case folded" — for both the rule table and the drain. What that single text
// is built with is `stripSQLComments`, one regular expression (`--[^\n]*`) that
// knows nothing about quotes, while `splitTopLevel` in the same file *does* track
// them. So a `--` inside a string literal takes the rest of its line out of the
// text the kernel judges, and the line that reads the window is that line.
//
// The file below is a correct, bounded data file: one statement, and it reads the
// window. `stripSQLComments` makes it a file that never reads the window, so
// `data-body-unbounded` fires against the sentence "one statement walks the whole
// table", which is false of it — and the remedy the refusal offers,
// `allow=data-body-unbounded reason=<how it bounds itself>`, then takes the window
// away for real, because `windowed()` reads the same mutilated text: the excepted
// body runs once over the whole table, in one transaction, which is the outage the
// rule exists to make impossible.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

const dashedData = `-- pkit: phase=data
-- pkit: batch=4
-- pkit: table=probe
UPDATE probe SET note = 'pending -- see the release note' WHERE id IN (SELECT id FROM batch)`

func TestADataFileThatReadsTheWindowIsNotJudgedFromTextItsLiteralsAte(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	admin := dbtest.Open(t, migrateURL)
	files := fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, note text NOT NULL DEFAULT '')")},
	}
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "dashed", Files: files}); err != nil {
		t.Fatalf("the owner's schema file: %v", err)
	}
	if _, err := admin.ExecContext(t.Context(), "INSERT INTO probe (id) SELECT g FROM generate_series(1, 30) g"); err != nil {
		t.Fatal(err)
	}
	files["000002_note.up.sql"] = &fstest.MapFile{Data: []byte(dashedData)}
	source := db.MigrationSource{Owner: "dashed", Files: files}

	if err := db.Migrate(t.Context(), migrateURL, source); err != nil {
		if strings.Contains(err.Error(), "data-body-unbounded") {
			t.Errorf("a phase=data file that reads the window was refused as unbounded: %v\n"+
				"its body names the window on the line a string literal's `--` hides from stripSQLComments, so the text the rule table and the drain read is not the body that would have run; the remedy the refusal offers (allow=data-body-unbounded) then drops the window for real and runs the whole table in one transaction", err)
		} else {
			t.Errorf("the bounded data file failed: %v", err)
		}
		return
	}
	// The other half of the claim: it was drained, in windows, not run once. Every
	// row written, and more than one transaction spent doing it.
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE note <> ''"); n != 30 {
		t.Errorf("%d of 30 rows were written", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'dashed' AND version = 2"); n != 1 {
		t.Errorf("the drain wrote %d history rows", n)
	}
}
