// migration_quoted_schema_test.go pins that a migration run reaches the namespace its own
// session resolves to when that namespace's name needs quotes: a space or a capital is legal
// in a PostgreSQL identifier, and a deployment whose schema is named that way is a deployment
// that exists, so the run has to find its ledger's home and the namespace its composition lock
// names. The lookup used to hand `current_schema()` to `to_regnamespace`, which parses an
// argument the way a statement parses an identifier — so those names answered no namespace,
// and the boot was refused with a sentence about a session that resolves to nothing.
//
// The names here are fixed rather than derived from the test name because the assertions are
// about the names themselves, and a fixed name is safe: this package's TestMain gives every
// process a database of its own, so no two runs of this case contend for one schema.
package db_test

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// quotedSchemaNames are the names one rule has to cover: two the identifier grammar only takes
// quoted, and one it takes as it stands. The third runs through the same code as the other two
// so that the control shows the fix moved nothing beside them, rather than in another case that
// reached its schema by another route.
var quotedSchemaNames = []struct {
	subtest string
	name    string
}{
	{"a name with a space", "Tenant Schema"},
	{"a name that is not lower case", "MixedCase"},
	{"a name the grammar takes as it stands", "plain_lowercase"},
}

// witnessTable holds the advisory locks the run's own backend held while its file ran. A second
// session polling pg_locks would have to know the OID it is checking and would race the run for
// it; the file below runs inside the run, behind its lock, so it records what was held then.
const witnessTable = "composition_lock_seen"

// ledgerOwner is the owner this case's one file is applied under.
const ledgerOwner = "quoted"

// TestAMigrationRunsInASchemaWhoseNameNeedsQuotes migrates one one-file composition into each
// name above and asks three things of every one: the run applied it instead of refusing, the
// runner's two tables landed in that namespace, and the composition lock it took named that
// namespace's OID rather than 0 or somebody else's.
func TestAMigrationRunsInASchemaWhoseNameNeedsQuotes(t *testing.T) {
	adminURL := os.Getenv("PLATFORMKIT_TEST_ADMIN_URL")
	if adminURL == "" {
		t.Fatalf("PLATFORMKIT_TEST_ADMIN_URL is unset; start the stack with `make up` and export the test URLs")
	}
	owner := dbtest.Open(t, adminURL)

	for _, schema := range quotedSchemaNames {
		t.Run(schema.subtest, func(t *testing.T) {
			migrateURL := quotedSchema(t, owner, adminURL, schema.name)
			namespace := namespaceOID(t, owner, schema.name)

			if err := db.MigrateWith(t.Context(), migrateURL, db.MigrationBudget{},
				db.MigrationSource{Owner: ledgerOwner, Files: fstest.MapFS{
					"000001_quoted.up.sql": {Data: []byte(`CREATE TABLE ` + witnessTable + ` AS
SELECT classid::bigint AS classid, objid::bigint AS objid
FROM pg_locks
WHERE pid = pg_backend_pid() AND locktype = 'advisory' AND granted`)},
				}}); err != nil {
				t.Fatalf("a run of %q refused: %v", schema.name, err)
			}

			assertRunnerTables(t, migrateURL, namespace)
			assertLedgerRow(t, migrateURL)
			assertLockNamesNamespace(t, migrateURL, namespace)
		})
	}
}

// quotedSchema makes schema exist in this package's own database, drops it again whatever the
// caller did with it, and answers a URL aimed at it.
func quotedSchema(t *testing.T, owner *sql.DB, adminURL, schema string) string {
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
	// The schema travels as a startup parameter of its own, quoted, because the `options`
	// spelling reaches the server as a whitespace-split command line and a quoted name with a
	// space in it does not survive the split. Only that pair is dropped: a packet naming
	// search_path twice would leave the server's choice of which one answers to chance.
	u.RawQuery = strings.Join(append(keptQuery(u.RawQuery, "options"),
		"search_path="+encodeOption(quoteIdent(schema))), "&")
	return u.String()
}

// namespaceOID is the OID pg_namespace holds for schema, read by name. The expectation is never
// built with `'<name>'::regnamespace` or to_regnamespace('<name>'): both parse their argument as
// an identifier and so answer nothing for exactly the names this case is about, which would let
// the case agree with the bug it exists to refuse.
func namespaceOID(t *testing.T, owner *sql.DB, schema string) int64 {
	t.Helper()
	var oid int64
	if err := owner.QueryRowContext(t.Context(),
		"SELECT oid::bigint FROM pg_namespace WHERE nspname = $1", schema).Scan(&oid); err != nil {
		t.Fatalf("read the namespace %q: %v", schema, err)
	}
	return oid
}

// assertRunnerTables asks which namespace the run's two tables came up in: the ledger and the
// backfill cursor beside it, both of them, because one run that put them somewhere other than
// where the lock points is the disagreement the lock exists to prevent.
func assertRunnerTables(t *testing.T, sessionURL string, namespace int64) {
	t.Helper()
	var landed []string
	rows, err := dbtest.Open(t, sessionURL).QueryContext(t.Context(), `
		SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.oid = $1 AND c.relname IN ('schema_migrations', 'schema_migration_backfill')
		ORDER BY 1`, namespace)
	if err != nil {
		t.Fatalf("list the runner's tables in namespace $1: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		landed = append(landed, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"schema_migration_backfill", "schema_migrations"}; !slices.Equal(landed, want) {
		t.Errorf("namespace %d holds [%s]; a run of this composition writes its ledger and its backfill cursor there, both of them",
			namespace, strings.Join(landed, ","))
	}
}

// assertLedgerRow reads the applied file back through a session aimed the same way the writer
// was, with nothing qualified: the row is where an unqualified reader of that namespace finds it.
func assertLedgerRow(t *testing.T, sessionURL string) {
	t.Helper()
	var applied string
	if err := dbtest.Open(t, sessionURL).QueryRowContext(t.Context(),
		"SELECT name FROM schema_migrations WHERE owner = $1", "quoted").Scan(&applied); err != nil {
		t.Fatalf("read back the ledger row this run wrote: %v", err)
	}
	if applied != "000001_quoted.up.sql" {
		t.Errorf("the ledger records %q, not the file the run applied", applied)
	}
}

// assertLockNamesNamespace reads the locks the run recorded while its file ran and asks for the
// composition key holding the namespace the run resolved to.
func assertLockNamesNamespace(t *testing.T, sessionURL string, namespace int64) {
	t.Helper()
	rows, err := dbtest.Open(t, sessionURL).QueryContext(t.Context(),
		"SELECT classid, objid FROM "+witnessTable)
	if err != nil {
		t.Fatalf("read the advisory locks the run held: %v", err)
	}
	defer rows.Close()
	held := 0
	for rows.Next() {
		var classid, objid int64
		if err := rows.Scan(&classid, &objid); err != nil {
			t.Fatal(err)
		}
		if classid != namespaceLockKey {
			continue
		}
		held++
		if objid == 0 {
			t.Error("the composition lock names no namespace")
		} else if objid != namespace {
			t.Errorf("the composition lock names namespace %d; the run resolved to %d, whose lock is the only one it may take", objid, namespace)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if held != 1 {
		t.Errorf("the run held the composition lock %d times; one run takes it once, under the namespace it resolved to", held)
	}
}
