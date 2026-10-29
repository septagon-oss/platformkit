// schema_path_first_existing_namespace_test.go pins which namespace a migration run resolves to
// when its session's search path names a namespace that does not exist before the one it migrates.
// The composition's namespace is read from current_schema(), which answers the first *existing*
// entry of the path rather than its first entry, and that is also the namespace an unqualified
// CREATE TABLE comes up in — so the ledger and the composition lock of a session whose path starts
// with a name nobody created still name one namespace. Resolving the path by its first entry
// instead answers no namespace at all there, and the run refuses a deployment whose schema exists
// one step later in its own session.
package db_test

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// pathLeadNamespace is the search path's first entry: a namespace this case never creates, so the
// server steps over it to the one below. It is absent rather than an existing namespace with
// nothing in it — an existing one is where an unqualified CREATE TABLE lands, and the whole of this
// case is which entry the server steps over.
const pathLeadNamespace = "kit_db_path_lead_absent"

// pathLeadSchemas are the namespaces a run has to reach from behind that absent entry: one whose
// name the identifier grammar only takes quoted, and one it takes as it stands. The second runs
// through the same two-entry path as the first, so what moved is the resolution and not the path.
var pathLeadSchemas = []struct {
	subtest string
	name    string
}{
	{"a quoted name behind an absent one", "Quoted Tenant Schema"},
	{"a lower-case name behind an absent one", "plain_behind_absent"},
}

// pathLeadOwner is the migration owner this case's one file is applied under, spelled apart from
// the other namespace cases' so that two runs of one database read their own ledger row back.
const pathLeadOwner = "pathlead"

// TestAMigrationResolvesToTheFirstExistingSchemaOfItsPath migrates one one-file composition into
// each name above through a path that leads with a namespace nobody created, and asks that the run
// reached the schema it names: its two tables landed there, its ledger row is readable there, and
// the composition lock it held while its file ran named that namespace's OID.
func TestAMigrationResolvesToTheFirstExistingSchemaOfItsPath(t *testing.T) {
	adminURL := os.Getenv("PLATFORMKIT_TEST_ADMIN_URL")
	if adminURL == "" {
		t.Fatalf("PLATFORMKIT_TEST_ADMIN_URL is unset; start the stack with `make up` and export the test URLs")
	}
	owner := dbtest.Open(t, adminURL)

	for _, schema := range pathLeadSchemas {
		t.Run(schema.subtest, func(t *testing.T) {
			migrateURL := pathBehindAbsent(t, owner, adminURL, schema.name)
			namespace := namespaceOID(t, owner, schema.name)

			// The path arrived as it was asked for: the server resolved this session past
			// the absent entry to the namespace below it, so what the run below is asked
			// about is a session whose path names two namespaces and resolves to the second.
			if got := dbtest.DeploymentSchema(t, dbtest.Open(t, migrateURL)); got != schema.name {
				t.Fatalf("a session on that path resolves to %q; the case is about one that resolves to %q",
					got, schema.name)
			}

			if err := db.MigrateWith(t.Context(), migrateURL, db.MigrationBudget{},
				db.MigrationSource{Owner: pathLeadOwner, Files: fstest.MapFS{
					"000001_pathlead.up.sql": {Data: []byte(`CREATE TABLE ` + witnessTable + ` AS
SELECT classid::bigint AS classid, objid::bigint AS objid
FROM pg_locks
WHERE pid = pg_backend_pid() AND locktype = 'advisory' AND granted`)},
				}}); err != nil {
				t.Fatalf("a run whose path leads with the absent %q and reaches %q refused: %v",
					pathLeadNamespace, schema.name, err)
			}

			assertRunnerTables(t, migrateURL, namespace)
			assertLedgerRowFromPath(t, migrateURL)
			assertLockNamesNamespace(t, migrateURL, namespace)
			assertNamespaceStillAbsent(t, owner, pathLeadNamespace)
		})
	}
}

// pathBehindAbsent makes schema exist in this package's own database, drops it again whatever the
// caller did with it, and answers a URL whose search_path names the absent pathLeadNamespace first
// and this namespace second. Both names are quoted and %20-encoded, because the value reaches the
// server percent-decoded as one string and a comma-separated pair carries it whole.
func pathBehindAbsent(t *testing.T, owner *sql.DB, adminURL, schema string) string {
	t.Helper()
	ctx := t.Context()
	if _, err := owner.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+quoteIdent(schema)+" CASCADE"); err != nil {
		t.Fatalf("clear the way for the schema %q: %v", schema, err)
	}
	if _, err := owner.ExecContext(ctx, "CREATE SCHEMA "+quoteIdent(schema)); err != nil {
		t.Fatalf("make the schema %q exist: %v", schema, err)
	}
	t.Cleanup(func() {
		if _, err := owner.ExecContext(context.WithoutCancel(t.Context()),
			"DROP SCHEMA IF EXISTS "+quoteIdent(schema)+" CASCADE"); err != nil {
			t.Errorf("the sub-case left the schema %q behind: %v", schema, err)
		}
	})

	u, err := url.Parse(adminURL)
	if err != nil {
		t.Fatalf("%q is not a URL: %v", adminURL, err)
	}
	path := quoteIdent(pathLeadNamespace) + "," + quoteIdent(schema)
	u.RawQuery = strings.Join(append(keptQuery(u.RawQuery, "options", "search_path"),
		"search_path="+encodeOption(path)), "&")
	return u.String()
}

// assertNamespaceStillAbsent asks that the namespace has not come into existence. A run reads
// the path its session was given and writes where that resolves; minting a namespace the session
// only named would put a ledger where no deployment asked for one.
func assertNamespaceStillAbsent(t *testing.T, owner *sql.DB, schema string) {
	t.Helper()
	var found bool
	if err := owner.QueryRowContext(t.Context(),
		"SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1)", schema).Scan(&found); err != nil {
		t.Fatalf("ask whether the namespace %q exists: %v", schema, err)
	}
	if found {
		t.Errorf("the run left the namespace %q behind; a session's path names where to write, it does not create it", schema)
	}
}

// assertLedgerRowFromPath reads the applied file back through a session aimed the same way the
// writer was, with nothing qualified: the row is where an unqualified reader of that namespace
// finds it, which is the reading a later boot of the same deployment asks.
func assertLedgerRowFromPath(t *testing.T, sessionURL string) {
	t.Helper()
	var applied string
	if err := dbtest.Open(t, sessionURL).QueryRowContext(t.Context(),
		"SELECT name FROM schema_migrations WHERE owner = $1", pathLeadOwner).Scan(&applied); err != nil {
		t.Fatalf("read back the ledger row this run wrote: %v", err)
	}
	if applied != "000001_pathlead.up.sql" {
		t.Errorf("the ledger records %q, not the file the run applied", applied)
	}
}
