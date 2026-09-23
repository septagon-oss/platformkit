package db_test

// review12_a_marker_the_reader_cannot_read_is_refused_and_never_ignored_test.go is the
// twelfth round's case for one sentence of the grammar:
//
//	migration_header.go: "A key outside this list is refused rather than ignored: a marker
//	  the runner would not read says 'this file was reviewed' about a file that was not."
//	migrations/README.md: "After the first line that is not a header line, a `-- pkit:`
//	  marker may not appear again — a marker the runner would not read claims a review the
//	  runner never did."
//
// The second rule protects the run *below* the header, and it is reached by matching
// `headerLine` again — the same expression, so it only ever refuses a marker the reader
// could have read. Nothing protects the run at the *top* from a spelling one whitespace away
// from the marker: `headerLine` is `^-- pkit:(.*)$`, so an extra space after the dashes, no
// space after them, a tab, an indent, or a space before the colon leaves a line that is not
// a header line, the body starts there, the rest of the marker run is not a marker either,
// and the file is read as a plain `phase=expand` file.
//
// For `phase=data` the two readings are not the same file. Measured at HEAD, six files whose
// only difference is the whitespace in the marker, each declaring the same data migration
// (`phase=data`, `batch=5000`, `table=probe`) over a body that does not read the window:
//
//	one space, the documented spelling  applied=0 err=rule data-body-unbounded: … one
//	                                     statement walks the whole table …
//	two spaces after the dashes         applied=1 err=<nil>
//	no space after the dashes           applied=1 err=<nil>
//	a tab after the dashes              applied=1 err=<nil>
//	an indent before the dashes          applied=1 err=<nil>
//	a space before the colon            applied=1 err=<nil>
//
// The five unread ones run the author's whole-table UPDATE as one transaction inside the
// migration — the harm `data-body-unbounded` exists to refuse, and the harm the brief names
// ("a backfill inside the migration's transaction holds every row") — and then record the
// version, which makes the bytes immutable: migrations/README.md's own remedy for an applied
// file is "add a new migration", and the marker the author already wrote can never be added.
// `phase=contract` fails closed by luck (the `drop-column` rule reads the SQL whatever the
// header said), so this is the one declaration whose misreading the rule table cannot catch.
//
// The assertion is that a file's kind does not depend on whitespace: every spelling of the
// declaration is either read or refused, and a run that refuses applies nothing of its owner.
// It can pass by widening the marker to the spellings a human writes (`^\s*--+\s*pkit:`), or
// by refusing any line shaped like a marker that the strict grammar did not read, which is
// what the sentence above already promises. The documented spelling passes it today.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestAMarkerTheReaderCannotReadIsRefusedAndNeverIgnored(t *testing.T) {
	// The same three declarations in the six spellings, none of which is a different
	// migration. The body names no window on purpose: with the declaration read that is
	// refused as unbounded, and with it unread it is a whole-table UPDATE in one
	// transaction, which is what the refusal was written for.
	declare := func(marker string) string {
		return marker + " phase=data\n" +
			marker + " batch=5000\n" +
			marker + " table=probe\n" +
			"UPDATE probe SET note = 'backfilled' WHERE note IS NULL"
	}
	spellings := []struct{ name, marker string }{
		{"the documented spelling", "-- pkit:"},
		{"two spaces after the dashes", "--  pkit:"},
		{"no space after the dashes", "--pkit:"},
		{"a tab after the dashes", "--\tpkit:"},
		{"an indent before the dashes", "   -- pkit:"},
		{"a space before the colon", "-- pkit :"},
	}
	for _, spelling := range spellings {
		t.Run(spelling.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{
				"000001_probe.up.sql": {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, note text); INSERT INTO probe (id) SELECT g FROM generate_series(1, 3) g")},
				"000002_fill.up.sql":  {Data: []byte(declare(spelling.marker))},
			}
			err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "spelling", Files: files})
			if err == nil {
				t.Fatalf("the spelling %q is no marker to the reader, and no marker to the reader means the file ran as a schema file: the whole-table UPDATE ran in one transaction and the version recorded it", spelling.name)
			}
			// The refusal is either the rule the declaration asks for or the marker the
			// reader could not read; both answer the operator, and neither is a silent
			// run of a different file.
			if !strings.Contains(err.Error(), "data-body-unbounded") && !strings.Contains(err.Error(), "pkit") {
				t.Errorf("the refusal says nothing about the declaration or the rule it broke, so the operator has no line to fix: %v", err)
			}
			// A refusal of a file's text refuses its owner before the runner connects, so
			// nothing of this migration is in the database — counted of the catalogue,
			// which answers whatever the guard decides.
			admin := dbtest.Open(t, migrateURL)
			if n := countRows(t, admin, "SELECT count(*) FROM pg_class WHERE relname = 'probe' AND relnamespace = current_schema()::regnamespace"); n != 0 {
				t.Errorf("%d relations named probe: the unread marker's file reached the server, one statement over every row", n)
			}
		})
	}
}
