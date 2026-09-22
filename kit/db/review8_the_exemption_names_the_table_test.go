package db_test

// review8_the_exemption_names_the_table_test.go is the tenth round's case for the reach of
// the ninth review's second finding, on the side of the line the finding did not stand on.
//
// The finding was that the exemption below `index-not-concurrent` compared the *spelling* of
// a table name: `newMigrationText` recorded `reCreateTable`'s capture verbatim, so
// `CREATE TABLE "probe"` recorded `"probe"` and `CREATE INDEX … ON probe` asked for `probe`
// and was refused — the file that ships its index in the file that creates the table, which
// is the remedy the refusal itself names, answered with a rule it had not broken. The fix
// reads the name, which is what `sqlIdent` exists for and what every other rule that acts on
// a captured name already does.
//
// `review8_index_exemption_spelling_test.go` covers the four spellings of *one* table. What
// it cannot say, because a grant and an exemption look identical from the inside, is that the
// exemption still stops at the table the file creates: a file that creates one table quoted
// and builds its index on another is a plain build on a table with readers, and that the
// spelling of the two names happens to agree changes nothing about the lock. So this case
// refuses that file, and asserts that the refusal took the file's own create with it — which
// is what the rule table's "a guard refuses a whole owner before any of it runs" promises for
// a file refused on its text.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestTheIndexExemptionDoesNotReachATableTheFileNeverNames(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	files := fstest.MapFS{
		"000001_seed.up.sql": {Data: []byte("CREATE TABLE other (id bigint PRIMARY KEY, t text)")},
		"000002_two.up.sql": {Data: []byte(`CREATE TABLE "probe" (id bigint PRIMARY KEY, t text);
CREATE INDEX other_t_idx ON other (t)`)},
	}
	err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "two", Files: files})
	if err == nil || !strings.Contains(err.Error(), "index-not-concurrent") {
		t.Fatalf("a file that indexes a table it does not create was excused by a create beside it: %v", err)
	}
	admin := dbtest.Open(t, migrateURL)
	// The rule reads the file's text, so it refuses before the runner connects and nothing
	// of the owner applied: the create the exemption was reached for is not here either.
	if n := countRows(t, admin, "SELECT count(*) FROM pg_class WHERE relname = 'probe'"); n != 0 {
		t.Errorf("%d relations named probe: a refused file let the statement before it apply", n)
	}
}
