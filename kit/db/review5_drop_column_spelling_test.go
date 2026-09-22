package db_test

// review5_drop_column_spelling_test.go is the sixth review's case for the one
// statement the guard table exists to move into a later release, spelled the way
// PostgreSQL lets an author spell it.
//
// migrations/README.md: "`drop-column` | `DROP COLUMN` in a file that is not
// `phase=contract` | it takes a name away from the release running right now".
// The same page says, two paragraphs below, why the neighbouring rule does not
// read a keyword: "The rule about a type change reads the clause inside an
// `ALTER TABLE`, not the word `COLUMN`, because PostgreSQL makes that keyword
// optional and both spellings are the same rewrite." `reAddColumnAction` does the
// same for `ADD [COLUMN]`. `reDropColumn` is the third ALTER TABLE action in the
// table and the only one that requires the keyword:
//
//	reDropColumn = `\bdrop\s+column\b`
//
// ALTER TABLE's grammar makes it optional there too, so
// `ALTER TABLE probe DROP b` is the same statement, taking the same name away
// from the release running right now, and no rule, marker or review sees it. The
// file applies as an ordinary expand file with nothing said about it.
//
// The first leg is the control: the same file with the keyword is refused today,
// which is what makes the second leg a finding about the spelling rather than a
// complaint about the rule.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// TestTheDropColumnRuleCatchesTheSpellingPostgresAllows.
func TestTheDropColumnRuleCatchesTheSpellingPostgresAllows(t *testing.T) {
	for _, tc := range []struct{ name, statement string }{
		{"the spelling the rule matches", "ALTER TABLE probe DROP COLUMN b"},
		{"the spelling PostgreSQL allows", "ALTER TABLE probe DROP b"},
		{"and with the qualifier PostgreSQL allows", "ALTER TABLE IF EXISTS ONLY probe DROP b CASCADE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{
				"000001_probe.up.sql": {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, b text)")},
			}
			admin := dbtest.Open(t, migrateURL)
			if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "shortdrop", Files: files}); err != nil {
				t.Fatalf("the owner's first file: %v", err)
			}
			files["000002_drop.up.sql"] = &fstest.MapFile{Data: []byte(tc.statement)}
			err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "shortdrop", Files: files})
			if err != nil {
				// Either honest answer refuses before the connection and names the rule:
				// the rule itself, or `unused-allow` once the file is marked contract.
				if !strings.Contains(err.Error(), "drop-column") {
					t.Errorf("the refusal %q does not name drop-column", err)
				}
			} else {
				t.Errorf("the file %q applied with no refusal at all", tc.statement)
			}
			// The column is the assertion that does not care which refusal the remedy
			// is: an expand file takes a name away from the release running now, and a
			// refused run has to leave it where it was.
			if n := countRows(t, admin, "SELECT count(*) FROM pg_attribute WHERE attrelid = 'probe'::regclass AND attname = 'b' AND NOT attisdropped"); n != 1 {
				t.Errorf("%q dropped the column from an expand file (%d present): the guard reads the keyword, and PostgreSQL does not require it", tc.statement, n)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'shortdrop' AND version = 2"); n != 0 {
				t.Errorf("the refused run wrote %d history rows for the file that takes a name away", n)
			}
		})
	}
}
