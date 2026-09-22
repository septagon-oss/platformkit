package migrations_test

// The fourth review's pin for migrations/000026_module_schema.up.sql.
//
// Every shipped test of that function runs under dbtest, which makes a schema for
// the one test and pins its own pair of default privileges beside it
// (kit/db/dbtest/dbtest.go:62-67). That is a deployment, and a fair one, but it is
// not the deployment this repository ships. The one this repository documents is
// apps/platformkit/postgres-init.sql: the migration role pins SELECT, INSERT,
// UPDATE, DELETE on tables and USAGE, SELECT on sequences for the application role
// IN SCHEMA public, and public is where the kernel's SQL and every reference
// module's tables land. No test in the tree runs the function against that layout,
// and the delivery's own report leaves "public and a module schema compared side by
// side" under Not verified.
//
// So this is the comparison the README promises and nothing measures — "the
// privileges that deployment pinned for it there … no module schema hands out more
// than that", and the CHANGELOG's "the privileges that deployment pinned for it
// there". It asks the catalog the same question of the same role in both namespaces
// for the same table shape and asserts the two privilege sets are equal, which is
// the property the namespace rule exists to deliver.
//
// What it does not pin: the tenant policy and the isolation it enforces
// (TestModuleSchemaOpensTheOwnerToTheApplicationRole owns those; a policy cannot
// even be exercised inside the transaction below, because only kit/db may write the
// setting a policy reads), and a privilege list wider than this namespace's own pin
// in general (review_round2 and review_round3 pin that, against a deployment that
// pins SELECT alone). What it does bite on is the mirror drifting from the
// deployment's rows in the deployment's own namespace: a dropped `… ON SEQUENCES`
// statement, a schema grant that stopped being discovered, a privilege arriving that
// this namespace never named (TRUNCATE, REFERENCES, TRIGGER or ALL), a grant option
// appearing where none was pinned, a default-privilege row mirrored that the
// namespace did not carry, and the schema owned by anyone but the migration role.
//
// Everything it writes lives in one transaction that is rolled back, including the
// CREATE OR REPLACE FUNCTION and the schema itself, so the shared database keeps the
// reference shape (two pg_default_acl rows, an empty public) whatever this test
// does. That is why it runs on the environment's base URLs rather than on a dbtest
// schema: the layout under test *is* the deployment's own namespace.

import (
	"context"
	"database/sql"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/migrations"
)

func TestModuleSchemaParityWithTheReferenceDeploymentsOwnNamespace(t *testing.T) {
	ctx := t.Context()
	adminURL := os.Getenv("PLATFORMKIT_TEST_ADMIN_URL")
	if adminURL == "" {
		t.Fatal("PLATFORMKIT_TEST_ADMIN_URL names no database to run the reference deployment against")
	}
	reader := dbtest.RoleOf(t, os.Getenv("PLATFORMKIT_TEST_DATABASE_URL"))
	if !bareIdentifier(reader) {
		t.Fatalf("the application role %q is not a name this file may write into a statement", reader)
	}
	// A name no other test, package or run can hold: 33 bytes of the grammar an
	// owner may use, and the schema is gone when the transaction is.
	owner := "z" + strings.ReplaceAll(uuid.NewString(), "-", "")

	pool := dbtest.Open(t, adminURL)
	conn, err := pool.Conn(ctx)
	if err != nil {
		t.Fatalf("one connection to the reference database: %v", err)
	}
	defer conn.Close()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin the one transaction everything lives in: %v", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			t.Errorf("roll the probe back: %v", err)
		}
	}()

	// The shipped function, verbatim, from the embedded source the runner applies.
	// PostgreSQL makes DDL transactional, so the rollback takes the function away
	// with the schema.
	body, err := fs.ReadFile(migrations.Source.Files, "000026_module_schema.up.sql")
	if err != nil {
		t.Fatalf("read the shipped migration: %v", err)
	}
	if _, err := tx.ExecContext(ctx, string(body)); err != nil {
		t.Fatalf("create the shipped function: %v", err)
	}

	deployment := currentNamespace(t, ctx, tx)
	// The control, and it is load-bearing: the deployment this repository ships
	// really did pin its pair for this namespace, granted by this very role. If it
	// had not, the parity below would be a comparison of two nothing-answers.
	want := "S=SELECT,S=USAGE,r=DELETE,r=INSERT,r=SELECT,r=UPDATE"
	if got := pinnedFor(t, ctx, tx, "current_schema()::regnamespace", reader); got != want {
		t.Fatalf("the premise is broken: %q pins %q for %q in %s, want the pair "+
			"apps/platformkit/postgres-init.sql writes (%q), so the parity below would compare nothing",
			adminURL, got, reader, deployment, want)
	}

	if _, err := tx.ExecContext(ctx, "SELECT platformkit_module_schema($1)", owner); err != nil {
		t.Fatalf("open a module schema as the deployment's migration role: %v", err)
	}
	// The same table shape in each namespace, both created after the pin: the
	// deployment's own namespace and the schema the function just opened.
	for _, statement := range []string{
		"CREATE TABLE " + deployment + ".items (tenant_id uuid NOT NULL, id serial NOT NULL, name text NOT NULL, PRIMARY KEY (tenant_id, id))",
		"CREATE TABLE " + owner + ".items (tenant_id uuid NOT NULL, id serial NOT NULL, name text NOT NULL, PRIMARY KEY (tenant_id, id))",
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			t.Fatalf("one table shape in both namespaces: %s: %v", statement, err)
		}
	}

	// The parity itself: the same reader, the same shape, the same answer in both
	// namespaces — privilege types and grant options together, as one string each,
	// read out of the ACL rather than through a helper that answers for privileges
	// a role holds as a member of another role.
	for _, c := range []struct {
		what                   string
		inDeployment, inModule string
		kind                   string
	}{
		{"the table", deployment + ".items", owner + ".items", "r"},
		{"its sequence", deployment + ".items_id_seq", owner + ".items_id_seq", "S"},
	} {
		by, want := granted(t, ctx, tx, c.inDeployment, reader, c.kind)
		_, got := granted(t, ctx, tx, c.inModule, reader, c.kind)
		if got != want {
			t.Errorf("platformkit_module_schema handed %q %s privileges on %s — %q — where the "+
				"deployment's own namespace hands %q — %q. README.md promises the privileges that "+
				"deployment pinned for that role beside the schema it opens and no more; %q is what "+
				"the namespace's rows named.", reader, c.what, c.inModule, got, c.inDeployment, want, by)
		}
	}
	for _, ask := range []struct {
		where string
		what  string
	}{
		{deployment, "the deployment's own namespace"},
		{owner, "the module schema"},
	} {
		var opened bool
		if err := tx.QueryRowContext(ctx,
			"SELECT pg_catalog.has_schema_privilege($2, $1, 'USAGE')", ask.where, reader).Scan(&opened); err != nil {
			t.Fatalf("ask whether %q may open %s: %v", reader, ask.where, err)
		}
		if !opened {
			t.Errorf("%q may not open %s (%q), although the namespace it runs in pins the table "+
				"privileges this schema mirrors; a reader that cannot reach the door reads nothing "+
				"behind it", reader, ask.what, ask.where)
		}
	}

	// The default privileges the module schema now carries are the namespace's own
	// rows for this grantee, row for row and privilege for privilege — and only
	// those, which is what "one namespace's rows and no union of several" is.
	if got, want := pinnedFor(t, ctx, tx, "to_regnamespace("+quote(owner)+")", reader),
		pinnedFor(t, ctx, tx, "current_schema()::regnamespace", reader); got != want {
		t.Errorf("%q carries %q as default privileges for %q where the namespace it opened beside "+
			"carries %q", owner, got, reader, want)
	}

	var ownedBy string
	if err := tx.QueryRowContext(ctx,
		"SELECT pg_get_userbyid(nspowner) FROM pg_namespace WHERE nspname = $1", owner).Scan(&ownedBy); err != nil {
		t.Fatalf("ask who owns %q: %v", owner, err)
	}
	var migrator string
	if err := tx.QueryRowContext(ctx, "SELECT current_user").Scan(&migrator); err != nil {
		t.Fatalf("ask who runs this transaction: %v", err)
	}
	if ownedBy != migrator {
		t.Errorf("%s is owned by %q, not by the migration role %q that opened it", owner, ownedBy, migrator)
	}

	// The grant in use, not merely in the catalog: the application role itself,
	// inside this transaction, writes the sequence-backed column and reads it back.
	// USAGE on that sequence is the one statement of the grantee loop a deployment
	// would not notice missing until an INSERT failed.
	if _, err := tx.ExecContext(ctx, "SET ROLE "+reader); err != nil {
		t.Fatalf("become the application role: %v", err)
	}
	result, err := tx.ExecContext(ctx,
		"INSERT INTO "+owner+".items (tenant_id, name) VALUES (gen_random_uuid(), $1)", "reference-row")
	if err != nil {
		t.Fatalf("the application role could not write %s.items: %v", owner, err)
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		t.Errorf("the application role's insert reported %d rows, want 1 (%v)", n, err)
	}
	var id int64
	if err := tx.QueryRowContext(ctx, "SELECT max(id) FROM "+owner+".items").Scan(&id); err != nil {
		t.Fatalf("read the sequence-backed column back as the application role: %v", err)
	}
	if id < 1 {
		t.Errorf("%s.items took id %d from its sequence, want the first value it handed out", owner, id)
	}
	if _, err := tx.ExecContext(ctx, "RESET ROLE"); err != nil {
		t.Fatalf("come back to the migration role: %v", err)
	}

	// Idempotent in the deployment's own namespace too: the same call again leaves
	// the same default-privilege rows standing.
	first := pinnedFor(t, ctx, tx, "to_regnamespace("+quote(owner)+")", reader)
	if _, err := tx.ExecContext(ctx, "SELECT platformkit_module_schema($1)", owner); err != nil {
		t.Fatalf("open the same module schema a second time: %v", err)
	}
	if again := pinnedFor(t, ctx, tx, "to_regnamespace("+quote(owner)+")", reader); again != first {
		t.Errorf("a second call changed %q's default privileges for %q from %q to %q", owner, reader, first, again)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("roll the probe back: %v", err)
	}
	// Nothing of this survives: not the schema, and not the function.
	var left int
	if err := conn.QueryRowContext(ctx,
		"SELECT count(*) FROM pg_namespace WHERE nspname = $1", owner).Scan(&left); err != nil {
		t.Fatalf("ask whether %q survived the rollback: %v", owner, err)
	}
	if left != 0 {
		t.Errorf("%q survived the rollback of the transaction that created it, so a migration that "+
			"fails after opening its schema leaves one the ledger does not name", owner)
	}
}

// pinnedFor is the default privileges one namespace holds for one grantee, spelled
// "kind=PRIVILEGE" and sorted, from rows the *current role* pinned. Which namespace
// is an expression rather than a value so the caller can name the deployment's own
// and a module's in the same shape.
func pinnedFor(t *testing.T, ctx context.Context, tx *sql.Tx, namespace, grantee string) string {
	t.Helper()
	var got string
	if err := tx.QueryRowContext(ctx, `
		SELECT coalesce(string_agg(d.defaclobjtype::text || '=' || a.privilege_type, ',' ORDER BY d.defaclobjtype, a.privilege_type), '')
		FROM pg_default_acl d CROSS JOIN LATERAL aclexplode(d.defaclacl) a
		WHERE d.defaclrole = current_user::regrole AND d.defaclnamespace = `+namespace+`
			AND a.grantee = $1::regrole`, grantee).Scan(&got); err != nil {
		t.Fatalf("read the default privileges %s holds for %q: %v", namespace, grantee, err)
	}
	return got
}

// granted reads one relation's ACL for one role straight out of pg_class, with the
// grant options visible, and reports which row it read. has_*_privilege was not
// enough here: it answers yes for a privilege reached through role membership or a
// schema grant, and this claim is about what the ACL of this relation says.
func granted(t *testing.T, ctx context.Context, tx *sql.Tx, relation, grantee, kind string) (row, privs string) {
	t.Helper()
	if err := tx.QueryRowContext(ctx, `
		SELECT $1::text, coalesce(string_agg(a.privilege_type || CASE WHEN a.is_grantable THEN ' GRANT' ELSE '' END, ',' ORDER BY a.privilege_type), '')
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		CROSS JOIN LATERAL aclexplode(c.relacl) a
		WHERE n.nspname || '.' || c.relname = $2 AND c.relkind = $3 AND a.grantee = $4::regrole`,
		relation, relation, kind, grantee).Scan(&row, &privs); err != nil {
		t.Fatalf("read %q's ACL for %q: %v", relation, grantee, err)
	}
	return row, privs
}

func currentNamespace(t *testing.T, ctx context.Context, tx *sql.Tx) string {
	t.Helper()
	var schema string
	if err := tx.QueryRowContext(ctx, "SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatalf("ask which namespace this transaction runs in: %v", err)
	}
	if !bareIdentifier(schema) {
		t.Fatalf("the reference namespace %q is not a bare identifier, so this deployment's layout "+
			"is not the one apps/platformkit/postgres-init.sql describes", schema)
	}
	return schema
}

// bareIdentifier is the shape an unquoted SQL identifier survives — the schema names
// and role names in this file come from the server and from the connection URL, and
// each is written into a statement.
func bareIdentifier(name string) bool {
	ok, err := regexp.MatchString(`^[a-z][a-z0-9_]*$`, name)
	return err == nil && ok
}

func quote(literal string) string { return "'" + strings.ReplaceAll(literal, "'", "''") + "'" }
