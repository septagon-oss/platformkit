package db_test

// review6_quoted_name_test.go is the seventh review's case for the claim ADR 0011
// makes about the rule table — "The rules read operations rather than spellings" —
// and migrations/README.md's version of it for the dropped column: "it is refused
// whatever the word after `COLUMN` was".
//
// Three rules find their operation by capturing the *name* the action carries:
// reDropAction (`DROP [COLUMN] <name>`), reAddColumnAction (`ADD [COLUMN] <name>`)
// and reColumnTypeClause (`ALTER [COLUMN] <name> TYPE`). All three capture it with
// `[a-z_]` as the first character, which is the spelling of a bare identifier and
// not the spelling PostgreSQL takes for a name that has to be quoted. A column whose
// name is a reserved word, or was created inside double quotes, can only ever be
// named quoted — `ALTER TABLE probe DROP "order"` is not a spelling an author chose
// over another, it is the only spelling that parses — and for those columns the three
// rules read nothing at all: the operation applies, the version is in the ledger, no
// rule is named and no marker is asked for.
//
// The harm is the one each rule's own row in migrations/README.md names: a full
// rewrite under ACCESS EXCLUSIVE for the type change, a rewrite that refuses every
// write for the NOT NULL column, a name taken away from the release running now for
// the drop.
//
// Every leg asserts through the catalogue and the ledger rather than through a
// message, and the unquoted control leg in each family is the branch that already
// passes — it localises the fault to the quoting, not to the question.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// quotedNameSchema: `b` and `n` are ordinary columns written with the quotes
// PostgreSQL allows for any name; `order` and `total` are names that cannot be
// written at all without them.
const quotedNameSchema = `CREATE TABLE probe (
	id bigint PRIMARY KEY,
	b text,
	n integer,
	"order" text,
	"total" integer
)`

func TestTheNameRulesReadTheOperationAndNotTheQuoting(t *testing.T) {
	for _, tc := range []struct {
		name, statement, rule string
	}{
		{
			name:      "a dropped column named bare, the control",
			statement: "ALTER TABLE probe DROP COLUMN b",
			rule:      "drop-column",
		},
		{
			name:      "a dropped column named with the quotes PostgreSQL takes",
			statement: "ALTER TABLE probe DROP \"b\"",
			rule:      "drop-column",
		},
		{
			name:      "a dropped column named with the keyword and the quotes together",
			statement: "ALTER TABLE probe DROP COLUMN \"b\"",
			rule:      "drop-column",
		},
		{
			name:      "a dropped column whose name cannot be written unquoted at all",
			statement: `ALTER TABLE probe DROP "order"`,
			rule:      "drop-column",
		},
		{
			name:      "a type change named bare, the control",
			statement: "ALTER TABLE probe ALTER COLUMN n TYPE bigint",
			rule:      "alter-column-type",
		},
		{
			name:      "a type change named with the quotes PostgreSQL takes",
			statement: `ALTER TABLE probe ALTER COLUMN "n" TYPE bigint`,
			rule:      "alter-column-type",
		},
		{
			name:      "a NOT NULL column named bare, the control",
			statement: "ALTER TABLE probe ADD c text NOT NULL",
			rule:      "add-column-not-null",
		},
		{
			name:      "a NOT NULL column named with the quotes PostgreSQL takes",
			statement: `ALTER TABLE probe ADD "c" text NOT NULL`,
			rule:      "add-column-not-null",
		},
		{
			// The third brief rule finds its table by failing to read it, which
			// is the direction that refuses rather than the one that ships: a nil
			// target fires. Pinned here so a fix for the three above cannot
			// "improve" this one into the other direction.
			name:      "an index built on a table named with the quotes PostgreSQL takes",
			statement: `CREATE INDEX probe_b_idx ON "probe" (b)`,
			rule:      "index-not-concurrent",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{"000001_probe.up.sql": {Data: []byte(quotedNameSchema)}}
			if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "quoted", Files: files}); err != nil {
				t.Fatalf("the owner's schema file: %v", err)
			}
			files["000002_alter.up.sql"] = &fstest.MapFile{Data: []byte(tc.statement)}
			err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "quoted", Files: files})
			admin := dbtest.Open(t, migrateURL)
			if err == nil {
				t.Errorf("%q applied with no refusal at all: the rule that is about it reads the name, and the name was written with the quotes PostgreSQL takes for it", tc.statement)
			} else if !strings.Contains(err.Error(), "rule "+tc.rule) {
				t.Errorf("the refusal %q does not name %s", err, tc.rule)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'quoted' AND version = 2"); n != 0 {
				t.Errorf("the refused run wrote %d history rows: %q applied", n, tc.statement)
			}
		})
	}
}

// TestAQuotedDropLeavesTheColumnWhereItIs states the same fault as the harm rather
// than as a refusal: the column a rule refused to see is the column the running
// release no longer has.
func TestAQuotedDropLeavesTheColumnWhereItIs(t *testing.T) {
	for _, tc := range []struct{ name, statement string }{
		{"named bare, the control", "ALTER TABLE probe DROP COLUMN b"},
		{"named quoted, the only spelling PostgreSQL takes for a reserved word", `ALTER TABLE probe DROP "order"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{"000001_probe.up.sql": {Data: []byte(quotedNameSchema)}}
			if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "quotedrop", Files: files}); err != nil {
				t.Fatalf("the owner's schema file: %v", err)
			}
			files["000002_alter.up.sql"] = &fstest.MapFile{Data: []byte(tc.statement)}
			if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "quotedrop", Files: files}); err == nil {
				t.Errorf("%q applied: it takes a name away from the release running now with no rule named and no marker asked", tc.statement)
			}
			admin := dbtest.Open(t, migrateURL)
			if n := countRows(t, admin, `SELECT count(*) FROM pg_attribute WHERE attrelid = 'probe'::regclass AND attname IN ('b', 'order') AND NOT attisdropped`); n != 2 {
				t.Errorf("%q left %d of the two columns in place: the guard read the spelling and not the operation", tc.statement, n)
			}
		})
	}
}
