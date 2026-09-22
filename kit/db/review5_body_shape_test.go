package db_test

// review5_body_shape_test.go is the sixth review's case for the one question the
// round made single: does this body read the batch window.
//
// kit/db/README.md says the answer is taken from one reading of the body and that
// the reading puts away "the contents of every string literal", so "a body that
// names the window only inside a value it writes is neither wrapped nor excused by
// accident", and migrations/README.md adds that "the apostrophe inside a comment is
// commentary, so neither can move the boundary of what the guard sees".
//
// `scanSQL` knows `--`, `'…'` and `"…"` and says it does not know dollar-quoted
// bodies, block comments or `E'…'` escapes. What it does not say is that the two
// readings it then produces fail in *both* directions for the question that is now
// asked only once:
//
//   - a dollar-quoted or block-commented body carrying an apostrophe opens a
//     literal that never closes, so `shape` puts away the real statements after it,
//     and a bounded file is refused as unbounded — and the remedy its refusal
//     offers (`allow=data-body-unbounded`) sends the body unwrapped, where the
//     window relation it names does not exist (42P01), after the owner's earlier
//     file has already applied. That is the harm the previous round closed for a
//     `--` inside a `'…'`, over again through the other spellings PostgreSQL
//     accepts;
//   - the same two constructs leave the text *in*, so a body whose only mention of
//     the window is data it is writing answers "yes", gets wrapped, and its one
//     whole-table statement runs once per window over every row of the table —
//     which is what the round's `migration.windowed` change exists to make
//     impossible, and what `windowed` still cannot see.
//
// Both legs assert through the rows: `passes` counts the transactions that wrote a
// row, so a body that ran once per window is visible whatever the runner printed.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// TestADataFileIsJudgedFromWhatPostgresReadsAsOneValue: a value written with
// dollar quotes, a block comment, or an E-string is one value or one comment to
// PostgreSQL, and neither its apostrophe nor the word inside it may move the
// boundary of what the guard and the drain read.
func TestADataFileIsJudgedFromWhatPostgresReadsAsOneValue(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{
			name: "the window read past an apostrophe in a dollar-quoted value",
			body: "UPDATE probe SET passes = passes + 1, a = $tag$it's the whole window$tag$ WHERE id IN (SELECT id FROM batch)",
		},
		{
			name: "the window read past an apostrophe in a block comment",
			body: "/* it's the backfill, one window at a time */\nUPDATE probe SET passes = passes + 1 WHERE id IN (SELECT id FROM batch)",
		},
		{
			name: "the window read past an escaped apostrophe in an E-string",
			body: "UPDATE probe SET passes = passes + 1, a = E'it\\'s the whole window' WHERE id IN (SELECT id FROM batch)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{
				"000001_probe.up.sql": {Data: []byte(`CREATE TABLE probe (id bigint PRIMARY KEY, a text, passes integer NOT NULL DEFAULT 0);
INSERT INTO probe (id) SELECT g FROM generate_series(1,25) g`)},
			}
			if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "dollarbody", Files: files}); err != nil {
				t.Fatalf("the owner's schema file: %v", err)
			}
			files["000002_backfill.up.sql"] = &fstest.MapFile{Data: []byte("-- pkit: phase=data\n-- pkit: batch=10\n-- pkit: table=probe\n" + tc.body)}
			if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "dollarbody", Files: files}); err != nil {
				t.Errorf("a bounded data file was refused: %v\nits body reads the window on the line the guard stopped reading; the remedy that refusal offers (allow=data-body-unbounded) then runs the same body with no window around it, where the relation it names does not exist", err)
				return
			}
			admin := dbtest.Open(t, migrateURL)
			if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE passes <> 1"); n != 0 {
				t.Errorf("%d of 25 rows were not written exactly once: the drain did not run this body over its windows", n)
			}
		})
	}
}

// TestADataFileWhoseWindowReferenceIsADollarsQuotedValueIsNotWrapped: the mirror.
// A body whose only `from batch` is inside a value it is writing does not read the
// window, so it must not be wrapped — the whole-table statement beside the window
// runs once per window, and the rows are written as many times as the table has
// windows.
func TestADataFileWhoseWindowReferenceIsADollarsQuotedValueIsNotWrapped(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"a single-quoted value", `'from batch'`},
		{"a dollar-quoted value", "$tag$from batch$tag$"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{
				"000001_probe.up.sql": {Data: []byte(`CREATE TABLE probe (id bigint PRIMARY KEY, a text, passes integer NOT NULL DEFAULT 0);
INSERT INTO probe (id) SELECT g FROM generate_series(1,25) g`)},
			}
			if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "dollarwrap", Files: files}); err != nil {
				t.Fatalf("the owner's schema file: %v", err)
			}
			files["000002_backfill.up.sql"] = &fstest.MapFile{Data: []byte("-- pkit: phase=data\n-- pkit: batch=10\n-- pkit: table=probe\nUPDATE probe SET passes = passes + 1, a = " + tc.value + " WHERE id > 0")}
			err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "dollarwrap", Files: files})
			admin := dbtest.Open(t, migrateURL)
			if err != nil && !strings.Contains(err.Error(), "rule data-body-unbounded") {
				t.Errorf("the file failed for something other than the rule refusal it needs: %v", err)
			}
			// The honest answers are the refusal (`data-body-unbounded`, nothing
			// written) or one unwrapped run (every row written once). Neither is "the
			// whole table, once per window".
			if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE passes > 1"); n != 0 {
				t.Errorf("%d of 25 rows were written more than once (max %d): the body names the window only inside a value it writes, and it was wrapped anyway, so its one whole-table statement ran once per window",
					n, countRows(t, admin, "SELECT coalesce(max(passes),0) FROM probe"))
			}
		})
	}
}

// TestAReasonThatNamesADeclarationLeavesTheColumnWhereItIs pins the trade the
// sixth round recorded: the grammar reads a `-- pkit:` pair written after a
// `reason=` sentence, so a file whose sentence quotes a declaration becomes that
// declaration, and the exception written beside it stops being one. The column has
// to be where it was either way.
func TestAReasonThatNamesADeclarationLeavesTheColumnWhereItIs(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	files := fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, b text)")},
	}
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "prose", Files: files}); err != nil {
		t.Fatalf("the owner's first file: %v", err)
	}
	files["000002_drop.up.sql"] = &fstest.MapFile{Data: []byte("-- pkit: allow=drop-column reason=ship it now, phase=contract expand=1 lands next\nALTER TABLE probe DROP COLUMN b")}
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "prose", Files: files}); err == nil {
		t.Error("a file whose sentence carried `phase=contract` applied its DROP COLUMN under an exception the declaration made valid")
	}
	admin := dbtest.Open(t, migrateURL)
	if n := countRows(t, admin, "SELECT count(*) FROM pg_attribute WHERE attrelid = 'probe'::regclass AND attname = 'b' AND NOT attisdropped"); n != 1 {
		t.Errorf("the column a reason sentence talked about a contract half is gone (%d present): reading the pair changed the kind of file the runner ran", n)
	}
}
