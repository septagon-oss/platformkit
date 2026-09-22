package db_test

// review8_index_exemption_spelling_test.go is the ninth review's case for the
// index-exemption below the `index-not-concurrent` rule.
//
// migrations/README.md's row for that rule says it "fires on `CREATE INDEX` without
// `CONCURRENTLY` on a table this file does not create outright". The exemption is the
// file's own create, and it is looked up by the *spelling* the two statements happened
// to use: `newMigrationText` records `reCreateTable`'s capture, which is `"probe"` when
// the file wrote the name quoted, and `reIndexTarget` reads `probe` when the build wrote
// it bare. PostgreSQL reads those two spellings as one table — it folds only ASCII and
// `"probe"` names exactly the table `probe` names — so a file that creates its table and
// builds its index in the same release is refused for building an index on a table
// nobody is reading yet.
//
// The direction matters. The exemption can only ever *withhold* a refusal, so nothing
// unsafe ships because of this: the fault is a false positive, and README's own sentence
// about false positives — "a statement inside a dollar-quoted body can be flagged, and
// the answer is the marker" — is the answer here too. But the remedy the refusal itself
// names, "ship the index in the file that creates the table", is the thing the file
// already did; the only way out is an `allow=` whose reason has to say the file does not
// do the thing the rule is about. A guard that forces an exception whose sentence is
// about the guard is a guard a reviewer stops reading.
//
// The same engine separates a name from the keyword it spells everywhere else it acts on
// a name: `sqlIdent` takes the quotes off a captured name to decide whether
// `DROP "constraint"` is a keyword or a column. Only this exemption compares the two
// spellings as text.
//
// The legs that pass today are the point: the same file, with one consistent spelling on
// both lines, is applied — so the fault is the punctuation on one of the two lines and not
// the rule.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestTheIndexExemptionReadsTheTableTheFileCreatesWhicheverSpellingItWrote(t *testing.T) {
	for _, tc := range []struct {
		name, create, index string
	}{
		{
			name:   "created bare, indexed bare, the control",
			create: `CREATE TABLE probe (id bigint PRIMARY KEY, t text)`,
			index:  `CREATE INDEX probe_t_idx ON probe (t)`,
		},
		{
			name:   "created quoted, indexed quoted",
			create: `CREATE TABLE "probe" (id bigint PRIMARY KEY, t text)`,
			index:  `CREATE INDEX probe_t_idx ON "probe" (t)`,
		},
		{
			name:   "created quoted, indexed bare",
			create: `CREATE TABLE "probe" (id bigint PRIMARY KEY, t text)`,
			index:  `CREATE INDEX probe_t_idx ON probe (t)`,
		},
		{
			name:   "created bare, indexed quoted",
			create: `CREATE TABLE probe (id bigint PRIMARY KEY, t text)`,
			index:  `CREATE INDEX probe_t_idx ON "probe" (t)`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{"000001_probe.up.sql": {Data: []byte(tc.create + ";\n" + tc.index)}}
			err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "spell", Files: files})
			admin := dbtest.Open(t, migrateURL)
			if err != nil && strings.Contains(err.Error(), "rule index-not-concurrent") {
				// Asserted on the catalogue rather than on the message, but the message
				// is what tells this case which rule to accuse, and only this rule. The run
				// refused before it connected, so there is no ledger to read afterwards.
				t.Fatalf("the file that creates %s and indexes it in the same statement list was refused by index-not-concurrent: %v", tc.create, err)
			} else if err != nil {
				t.Fatalf("the file was refused by something else: %v", err)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM pg_indexes WHERE tablename = 'probe' AND indexname = 'probe_t_idx'"); n != 1 {
				t.Errorf("%d of the file's own indexes exist: the release that creates a table may build its index", n)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'spell'"); n != 1 {
				t.Errorf("%d history rows: the file did not apply", n)
			}
		})
	}
}
