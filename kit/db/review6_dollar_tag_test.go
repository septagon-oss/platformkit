package db_test

// review6_dollar_tag_test.go is the seventh review's case for the sentence
// kit/db/README.md and migrations/README.md close with: the window question is asked
// of a reading that puts away "the contents of every string literal", so "a body that
// names the window only inside a value it is writing does not [get its window]" and
// "none of them can move the boundary of what the guard sees".
//
// `dollarTag` accepts a tag of "`$`, a tag, `$`", where the tag takes "the characters
// a name takes". PostgreSQL's rule for a dollar-quote tag is the rule for a *quoted*
// identifier, which in a UTF-8 database includes accented letters:
//
//	=> select length($é$abc$é$);  → 3
//
// So `$atualização$ … $atualização$` is one value to the server and not a tag to the
// reader. That is the round's own finding 2 — a body whose only window reference is
// data it writes is wrapped anyway, and its one whole-table statement runs once per
// window over every row — through a spelling its Limits names as "conservatively not a
// tag" without noticing which way the conservatism fails: not reading a tag leaves the
// tag's contents *in* the shape the question is asked of.
//
// The legs assert on `passes`, the number of transactions that wrote a row, so no
// message can argue with them, and the ASCII-tag control is the branch that already
// passes: it shows the question is asked correctly and only its reader is one
// construct short.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestADollarValueIsOneValueWhateverCharactersItsTagCarries(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"a value tagged with the ASCII characters the reader knows", "$tag$from batch$tag$"},
		{"a value tagged with the accented letters PostgreSQL takes", "$atualização$from batch$atualização$"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{
				"000001_probe.up.sql": {Data: []byte(`CREATE TABLE probe (id bigint PRIMARY KEY, note text, passes integer NOT NULL DEFAULT 0);
INSERT INTO probe (id) SELECT g FROM generate_series(1,25) g`)},
			}
			if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "tagchars", Files: files}); err != nil {
				t.Fatalf("the owner's schema file: %v", err)
			}
			files["000002_backfill.up.sql"] = &fstest.MapFile{Data: []byte("-- pkit: phase=data\n-- pkit: batch=10\n-- pkit: table=probe\nUPDATE probe SET passes = passes + 1, note = " + tc.value + " WHERE id > 0")}
			err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "tagchars", Files: files})
			admin := dbtest.Open(t, migrateURL)
			if err != nil && !strings.Contains(err.Error(), "rule data-body-unbounded") {
				t.Errorf("the file failed for something other than the rule refusal it needs: %v", err)
			}
			// The honest answers are the refusal (nothing written) or one unwrapped
			// run (every row written once). "The whole table, once per window" is
			// neither, and is what a reader that did not see the tag produces.
			if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE passes > 1"); n != 0 {
				t.Errorf("%d of 25 rows were written more than once (max %d): the body names the window only inside a value it writes, and it was wrapped anyway, so its one whole-table statement ran once per window",
					n, countRows(t, admin, "SELECT coalesce(max(passes),0) FROM probe"))
			}
		})
	}
}
