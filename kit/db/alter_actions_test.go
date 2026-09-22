package db_test

// alter_actions_test.go is the delivery's own case for the fourth rule reading an
// action rather than a keyword, in both directions.
//
// migrations/README.md: "`drop-column` | `DROP [COLUMN] …` in a file that is not
// `phase=contract` | it takes a name away from the release running right now".
// PostgreSQL makes that keyword optional for `DROP` as it does for a type change and
// for `ADD [COLUMN]`, so the guard reads the action inside the `ALTER TABLE` — which
// leaves two things to get right at once. Firing on the operation must not widen into
// firing on every `DROP`: the words `ALTER TABLE` puts after `DROP` for something that
// is not a column (`CONSTRAINT`, `IDENTITY`, `NOT NULL`, `DEFAULT`) take no name away,
// and a bare `DROP TABLE` or `DROP INDEX` is a different statement about a different
// object. A rule that refused those would stop the ordinary release to police a
// statement it is not about, and an author with a real reason would answer it with a
// marker, which is how a rule loses its meaning.
//
// Every leg is asserted through the catalogue and the ledger: the column is where a
// refused run left it, and an accepted file is in the history.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

const alterTableSchema = `CREATE TABLE probe (
	id bigint PRIMARY KEY,
	a text NOT NULL DEFAULT 'x',
	b text NOT NULL DEFAULT 'y',
	seq integer GENERATED ALWAYS AS IDENTITY,
	CONSTRAINT probe_b_not_empty CHECK (b <> '')
);
CREATE INDEX probe_a_idx ON probe (a);
CREATE TABLE other (id bigint PRIMARY KEY)`

func TestTheDropColumnRuleReadsTheActionAndNotEveryDrop(t *testing.T) {
	for _, tc := range []struct {
		name           string
		statement      string
		takesANameAway bool
	}{
		{"the keyword the rule matched before", "ALTER TABLE probe DROP COLUMN b", true},
		{"the keyword PostgreSQL does not require", "ALTER TABLE probe DROP b", true},
		{"and the qualifiers PostgreSQL does not require", "ALTER TABLE IF EXISTS ONLY probe DROP IF EXISTS b CASCADE", true},
		{"the action beside an ordinary one", "ALTER TABLE probe ADD COLUMN c text, DROP b", true},
		{"a constraint", "ALTER TABLE probe DROP CONSTRAINT probe_b_not_empty", false},
		// The three quoted legs, in one pair of ideas. `"order"` is a name PostgreSQL takes
		// only quoted, so a rule that reads the name has to read it quoted; `"constraint"`
		// is the keyword's own word used as a column's name, which PostgreSQL reads as the
		// name and never as the keyword, so the exemption above belongs to the bare word
		// alone; and `"a;b"` is a name that carries the separator the statement is split on.
		{"a name only the quotes PostgreSQL takes can write", `ALTER TABLE probe DROP "order"`, true},
		{"a quoted name that also spells the keyword", `ALTER TABLE probe DROP "constraint"`, true},
		{"a name whose own spelling carries the separator", `ALTER TABLE probe DROP "a;b"`, true},
		{"a NOT NULL the release no longer wants", "ALTER TABLE probe ALTER COLUMN b DROP NOT NULL", false},
		{"a DEFAULT", "ALTER TABLE probe ALTER COLUMN a DROP DEFAULT", false},
		{"an identity", "ALTER TABLE probe ALTER COLUMN seq DROP IDENTITY IF EXISTS", false},
		{"an index, another statement about another object", "DROP INDEX probe_a_idx", false},
		{"a table, another statement about another object", "DROP TABLE other", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{"000001_probe.up.sql": {Data: []byte(alterTableSchema)}}
			if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "actions", Files: files}); err != nil {
				t.Fatalf("the owner's schema file: %v", err)
			}
			files["000002_alter.up.sql"] = &fstest.MapFile{Data: []byte(tc.statement)}
			err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "actions", Files: files})
			admin := dbtest.Open(t, migrateURL)
			if !tc.takesANameAway {
				if err != nil {
					t.Errorf("the guard refused a statement it is not about: %v\n%s\nrefusing these is how a rule whose name says `column` comes to police every DROP in the release, and the marker that answers it is one a reviewer cannot read", err, tc.statement)
				}
				if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'actions' AND version = 2"); n != 1 {
					t.Errorf("the file the guard is not about wrote %d history rows: it did not apply", n)
				}
				return
			}
			if err == nil {
				t.Errorf("%q applied: it takes a name away from the release running now, and PostgreSQL does not require the keyword the guard was reading", tc.statement)
			} else if !strings.Contains(err.Error(), "drop-column") {
				t.Errorf("the refusal %q does not name drop-column", err)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM pg_attribute WHERE attrelid = 'probe'::regclass AND attname = 'b' AND NOT attisdropped"); n != 1 {
				t.Errorf("%q dropped the column from an expand file (%d present)", tc.statement, n)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'actions' AND version = 2"); n != 0 {
				t.Errorf("the refused run wrote %d history rows", n)
			}
		})
	}
}
