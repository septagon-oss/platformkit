package db_test

// split_boundary_test.go is the ninth round's case for the class the eighth review's
// second finding belongs to: a construct the statement splitter mis-reads moves the
// boundary of every statement after it, and a rule anchored at the front of a statement
// (`^alter table`) then reads no action at all — no refusal, no marker offered, and the
// drop or the rewrite in the ledger. The review's own file,
// review7_quote_parity_test.go, pins the spelling it found: one lone double quote inside a
// `$tag$ … $tag$` body, which the splitter had begun counting as the start of a name.
//
// These two legs are the other two ways the same boundary moved, and they are here because
// the fix is one decision — the splitter asks the same scanner that scanSQL asks where a
// construct ends — and a fix asserted through one spelling is a fix through one spelling:
//
//   - an unbalanced parenthesis inside a dollar body. The body is one value, so its
//     parentheses are data; counted, the depth they left suppressed every cut after them.
//     The statement is legal SQL, because a parenthesis inside a dollar-quoted body is
//     inside a string, which is why the broken reading runs the file rather than refusing it;
//   - an `E'…'` whose escape carries an apostrophe. The value ends where its author ended
//     it, and a splitter that stops at the escape takes the rest of the literal — quotes and
//     all — as SQL.
//
// Each leg asserts the refusal names its rule, that the refused version wrote no history
// row, and the catalogue fact the statement would have changed, so the case does not turn
// on any message the fix prints.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestAValueEndsWhereTheSplitterThinksItEnds(t *testing.T) {
	for _, tc := range []struct{ name, first, rule, holds string }{
		{
			name:  "a dollar body carrying one unmatched parenthesis",
			first: "CREATE FUNCTION probe_noop() RETURNS void LANGUAGE plpgsql AS $$\nBEGIN\n  -- one unmatched ( is data inside a body\n  RETURN;\nEND\n$$;\n",
			rule:  "drop-column",
			holds: "SELECT count(*) FROM pg_attribute WHERE attrelid = 'probe'::regclass AND attname = 'b' AND NOT attisdropped",
		},
		{
			name:  "an escape string whose apostrophe is escaped and whose text carries a double quote",
			first: "COMMENT ON TABLE probe IS E'an escaped \\' apostrophe and a \" double quote';\n",
			rule:  "drop-column",
			holds: "SELECT count(*) FROM pg_attribute WHERE attrelid = 'probe'::regclass AND attname = 'b' AND NOT attisdropped",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{"000001_probe.up.sql": {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, b text, n integer)")}}
			if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "boundary", Files: files}); err != nil {
				t.Fatalf("the owner's schema file: %v", err)
			}
			files["000002_value_then_drop.up.sql"] = &fstest.MapFile{Data: []byte(tc.first + "ALTER TABLE probe DROP b;")}
			err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "boundary", Files: files})
			admin := dbtest.Open(t, migrateURL)
			if err == nil {
				t.Errorf("the file applied with no refusal and no marker offered: the value above the drop was read as ending somewhere the server does not end it, and the statement after it stopped being one the rule reads")
			} else if !strings.Contains(err.Error(), "rule "+tc.rule) {
				t.Errorf("the refusal %q does not name %s", err, tc.rule)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'boundary' AND version = 2"); n != 0 {
				t.Errorf("the refused run wrote %d history rows", n)
			}
			if n := countRows(t, admin, tc.holds); n != 1 {
				t.Errorf("the schema holds %d rows of the fact %q, want 1: the drop reached PostgreSQL, which is what the boundary decides", n, tc.holds)
			}
		})
	}
}
