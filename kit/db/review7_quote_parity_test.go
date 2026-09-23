package db_test

// review7_quote_parity_test.go is the eighth review's case for a sentence
// migrations/README.md states about the guard's own reading: the comment and literal
// constructs it knows — `--`, `/* … */`, `$tag$ … $tag$`, `E'…'` — "None of them can move
// the boundary of what the guard sees."
//
// One of them can, and it is the one 97678f7 taught the statement splitter to count.
// `splitTopLevel` now toggles a `named` flag on every `"` it meets, and it meets the
// bytes of a dollar-quoted body whole: `scanSQL` keeps a `$tag$ … $tag$` value intact in
// `plain` — the text `splitStatements` is given — because a value is one value to the
// server. So the double quotes *inside* a function body are counted, and a body holding
// an odd number of them leaves the splitter convinced the rest of the file is inside a
// name: no `;` after it cuts at all, and the `ALTER TABLE` that follows is no longer at
// the start of any statement. `drop-column` and `alter-column-type` anchor their
// predicate on `^alter\s+table\b`, so both stop seeing the action; `add-column-not-null`
// is not anchored and survives, which is why one of the three legs below passes today
// and why the escape is about the anchor rather than about the name.
//
// This is the fault the same commit set out to close, one level earlier, and it is new in
// it: on f1443b0 the same file was refused, because a double quote meant nothing to the
// splitter and the statement boundary after the function body was where the server puts
// it. The two controls below are that fact — the same function with no double quote, and
// the same function with its quotes in pairs, both refused today — so the third leg is a
// finding about the parity of a quote the reader should not be counting here at all, not
// about the rule. Measured both ways: with the previous `splitTopLevel` restored in a
// copy of the tree, all five legs of this case PASS (and the two legs 97678f7 added for
// its own fix then FAIL, which is that commit's own `Verified:` paragraph reproduced); on
// the delivered tree, two of the five fail. Nothing in this repository trips it today —
// `grep -rln --include='*.sql' '\$\$' migrations modules/*/migrations` names only
// `migrations/000001_tenancy.up.sql`, and its three dollar bodies carry no double quote
// at all — so this is a guard that mis-reads a future file, in the one direction no
// marker can be asked for.
//
// README's own remedy for a dollar body the guard reads wrongly is the exception marker,
// and a marker has to be *asked for by name*: this file is refused by nothing, so nothing
// is excepted, the marker is never offered, and the version goes into the ledger with the
// drop applied. The legs assert the refusal, the absent history row and the column that
// has to still be there; none of them reads a message the broken file does not print.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// dollarBody is an ordinary expand file: a trigger function written as the server wants
// it written, and one ALTER TABLE after it. `$1` varies only the double quotes the body
// carries.
func dollarBody(quotes, alter string) string {
	return `CREATE OR REPLACE FUNCTION probe_note() RETURNS text AS $$
BEGIN
  -- the ` + quotes + ` column goes away in the statement below
  RETURN NULL;
END
$$ LANGUAGE plpgsql;
` + alter
}

func TestADollarBodyMovesNeitherTheStatementBoundaryNorTheRules(t *testing.T) {
	for _, tc := range []struct {
		name, quotes, statement, rule, holds string
		want                                 int
	}{
		{
			name:      "a body with no double quote, the control",
			quotes:    "old",
			statement: "ALTER TABLE probe DROP b;",
			rule:      "drop-column",
			holds:     "SELECT count(*) FROM pg_attribute WHERE attrelid = 'probe'::regclass AND attname = 'b' AND NOT attisdropped",
			want:      1,
		},
		{
			name:      "a body whose double quotes come in pairs, the control",
			quotes:    `""old""`,
			statement: "ALTER TABLE probe DROP b;",
			rule:      "drop-column",
			holds:     "SELECT count(*) FROM pg_attribute WHERE attrelid = 'probe'::regclass AND attname = 'b' AND NOT attisdropped",
			want:      1,
		},
		{
			name:      "a body carrying one double quote",
			quotes:    `"old`,
			statement: "ALTER TABLE probe DROP b;",
			rule:      "drop-column",
			holds:     "SELECT count(*) FROM pg_attribute WHERE attrelid = 'probe'::regclass AND attname = 'b' AND NOT attisdropped",
			want:      1,
		},
		{
			name:      "a body carrying one double quote, before a type change",
			quotes:    `"old`,
			statement: "ALTER TABLE probe ALTER COLUMN n TYPE varchar(20);",
			rule:      "alter-column-type",
			holds:     "SELECT count(*) FROM pg_attribute WHERE attrelid = 'probe'::regclass AND attname = 'n' AND atttypid = 'integer'::regtype",
			want:      1,
		},
		{
			name:      "a body carrying one double quote, before a NOT NULL column",
			quotes:    `"old`,
			statement: "ALTER TABLE probe ADD c text NOT NULL;",
			rule:      "add-column-not-null",
			holds:     "SELECT count(*) FROM pg_attribute WHERE attrelid = 'probe'::regclass AND attname = 'c' AND NOT attisdropped",
			want:      0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{"000001_probe.up.sql": {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, b text, n integer)")}}
			if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "parity", Files: files}); err != nil {
				t.Fatalf("the owner's schema file: %v", err)
			}
			files["000002_function_and_alter.up.sql"] = &fstest.MapFile{Data: []byte(dollarBody(tc.quotes, tc.statement))}
			err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "parity", Files: files})
			admin := dbtest.Open(t, migrateURL)
			if err == nil {
				t.Errorf("the file applied with no refusal and no marker offered: the statement splitter stopped cutting where the dollar body's quotes left off, so the rule about %s never saw the statement", tc.rule)
			} else if !strings.Contains(err.Error(), "rule "+tc.rule) {
				t.Errorf("the refusal %q does not name %s", err, tc.rule)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'parity' AND version = 2"); n != 0 {
				t.Errorf("the refused run wrote %d history rows", n)
			}
			if n := countRows(t, admin, tc.holds); n != tc.want {
				t.Errorf("the schema holds %d rows of the fact %q, want %d: %s reached PostgreSQL", n, tc.holds, tc.want, tc.statement)
			}
		})
	}
}
