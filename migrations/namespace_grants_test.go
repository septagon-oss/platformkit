package migrations_test

// The third review's cases for migrations/000026_module_schema.up.sql and
// dbtest.TenantTablesSQL. Two claims are checked here that no shipped test
// reaches, found the same way the first two reviews found theirs: ask what stays
// green if the statement is wrong, then run the case.
//
// TestModuleSchemaGrantsNoPrivilegeTheDeploymentPinnedElsewhere FAILS as filed.
// The function reads a grantee's privilege list out of every pg_default_acl row
// the migration role owns for that object kind, with no filter on
// defaclnamespace, and unions them. PostgreSQL itself does not: a default
// privilege pinned IN SCHEMA applies to that namespace and to no other (measured
// in the report beside this file). So a role the deployment pinned SELECT in the
// namespace it can reach, and INSERT in a second namespace of the same migration
// role, arrives inside every module schema with both — a privilege the
// deployment never handed that role anywhere. That contradicts the two sentences
// the function's header and migrations/README.md carry as the reason the
// privilege list is read rather than written ("a role the deployment pinned to
// SELECT reads a module's tables and does not write them"; "no module schema
// hands out more than that"), and it is the same escalation the second review's
// file pins, in the one dimension that file does not cover.
//
// The control is the first half of the test and is load-bearing: it asserts what
// PostgreSQL hands that role in each of the deployment's own namespaces, so the
// probe below cannot pass because the two pins never took.
//
// TestMigrateLeavesNoSchemaAndNoLedgerRowWhenAMigrationRefuses passes. It pins
// the sentence every refusal in 000026 leans on — "a refusal never leaves a
// half-open schema" — at the boundary that actually has to hold: a module's own
// revision file that opens its schema and then fails. The refusal only writes
// nothing because kit/db's applyMigration runs one file in one transaction and
// the ledger INSERT is in that same transaction, and nothing in this repository
// said so until a shipped SQL file depended on it.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/migrations"
)

// TestModuleSchemaGrantsNoPrivilegeTheDeploymentPinnedElsewhere. One read role,
// two namespaces of the one migration role, one privilege in each. The module
// schema may hand it either of those, or neither; it may not hand it both,
// because the deployment never did.
func TestModuleSchemaGrantsNoPrivilegeTheDeploymentPinnedElsewhere(t *testing.T) {
	ctx := t.Context()
	admin, _ := dbtest.Schema(t)
	deployment := dbtest.DeploymentSchema(t, admin)
	// Each derived name keeps the length of the schema it is cut from, which
	// dbtest guarantees is at most 63 bytes: PostgreSQL shortens a longer one
	// silently, and the DROP ROLE below would then name a role that was never
	// created.
	other := "z" + deployment[1:]
	reader := "rr" + deployment[2:]
	owner := moduleOwner(t, admin, 'u')

	// Registered after moduleOwner's, so it runs before it (cleanups are LIFO),
	// to remove every privilege this role holds anywhere before asking Postgres
	// to end its life: it refuses one that still holds a grant on an object that
	// stands, in this test's own namespace as well as in the module's.
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, statement := range []string{
			"ALTER DEFAULT PRIVILEGES IN SCHEMA " + deployment + " REVOKE ALL ON TABLES FROM " + reader,
			"ALTER DEFAULT PRIVILEGES IN SCHEMA " + other + " REVOKE ALL ON TABLES FROM " + reader,
			"REVOKE ALL ON ALL TABLES IN SCHEMA " + deployment + " FROM " + reader,
			"DROP SCHEMA IF EXISTS " + owner + " CASCADE",
			"DROP SCHEMA IF EXISTS " + other + " CASCADE",
			"DROP ROLE IF EXISTS " + reader,
		} {
			if _, err := admin.ExecContext(cleanup, statement); err != nil {
				t.Errorf("cleanup %q: %v", statement, err)
			}
		}
	})

	for _, statement := range []string{
		"CREATE ROLE " + reader + " NOSUPERUSER NOBYPASSRLS",
		"CREATE SCHEMA " + other,
		// The deployment's own two statements, one namespace each: this role may
		// read what appears in the namespace it can reach, and may write what
		// appears in the other one. Nowhere does it do both.
		"ALTER DEFAULT PRIVILEGES IN SCHEMA " + deployment + " GRANT SELECT ON TABLES TO " + reader,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA " + other + " GRANT INSERT ON TABLES TO " + reader,
	} {
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			t.Fatalf("stand up the deployment's two namespaces: %s: %v", statement, err)
		}
	}
	for _, statement := range []string{
		"CREATE TABLE " + deployment + ".guard (id bigint)",
		"CREATE TABLE " + other + ".counter (id bigint)",
	} {
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			t.Fatalf("a table in each namespace: %s: %v", statement, err)
		}
	}

	// The control: PostgreSQL scopes a namespace-specific default privilege to
	// that namespace, so this role holds exactly one table privilege in each of
	// the deployment's namespaces and never both in one.
	for _, c := range []struct {
		table string
		priv  string
		want  bool
	}{
		{deployment + ".guard", "SELECT", true},
		{deployment + ".guard", "INSERT", false},
		{other + ".counter", "SELECT", false},
		{other + ".counter", "INSERT", true},
	} {
		if got := holds(t, ctx, admin, reader, c.table, c.priv); got != c.want {
			t.Fatalf("the premise is broken: %s.%s answered %t, want %t — the deployment's own "+
				"namespace-specific defaults are not in place, so the probe below would prove nothing",
				c.priv, c.table, got, c.want)
		}
	}

	if _, err := admin.ExecContext(ctx, "SELECT platformkit_module_schema($1)", owner); err != nil {
		t.Fatalf("open a schema owned as %q: %v", owner, err)
	}
	for _, statement := range []string{
		fmt.Sprintf("CREATE TABLE %s.items (tenant_id uuid NOT NULL, id uuid NOT NULL, PRIMARY KEY (tenant_id, id))", owner),
		fmt.Sprintf("ALTER TABLE %s.items ENABLE ROW LEVEL SECURITY", owner),
		fmt.Sprintf("ALTER TABLE %s.items FORCE ROW LEVEL SECURITY", owner),
		fmt.Sprintf("CREATE POLICY items_tenant ON %s.items USING (platformkit_tenant_match(tenant_id)) WITH CHECK (platformkit_tenant_match(tenant_id))", owner),
	} {
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			t.Fatalf("a module's own revision: %s: %v", statement, err)
		}
	}

	// The probe. SELECT is what the deployment pinned for this role in the
	// namespace it can reach, so it is expected here; INSERT is what it pinned in
	// a different namespace, which PostgreSQL handed to nothing that lives here.
	for _, c := range []struct {
		priv string
		want bool
		why  string
	}{
		{"SELECT", true, "the deployment pinned SELECT on tables for this role in " + deployment +
			", the namespace it can reach, and a module's schema is opened beside it"},
		{"INSERT", false, "the deployment pinned INSERT only in " + other + ", and PostgreSQL gave " +
			deployment + ".guard no INSERT at all; a role that cannot write one of the deployment's " +
			"own tables must not find it can write a module's"},
	} {
		if got := holds(t, ctx, admin, reader, owner+".items", c.priv); got != c.want {
			t.Errorf("platformkit_module_schema handed %q %s on %s.items, want %t: %s. The grantee "+
				"loop unions the privilege types of every pg_default_acl row that names the grantee, "+
				"whatever schema that row is pinned to, while 000026's header and migrations/README.md "+
				"both promise that a module's schema hands out no privilege wider than the deployment's "+
				"own answer for that role.",
				reader, c.priv, owner, c.want, c.why)
		}
	}
}

// TestMigrateLeavesNoSchemaAndNoLedgerRowWhenAMigrationRefuses runs a module's
// own first revision through db.Migrate: the one line that opens its schema, a
// table, and a statement that fails. Everything before the failure has to
// disappear with it — the schema, its default privileges and the history row —
// because the walk finds a module's tables through the ledger, so a schema that
// stands unnamed by a refusal would be a namespace the tenant-scope check never
// enters.
func TestMigrateLeavesNoSchemaAndNoLedgerRowWhenAMigrationRefuses(t *testing.T) {
	ctx := t.Context()
	adminURL, _ := dbtest.URLs(t)
	if err := db.Migrate(ctx, adminURL, migrations.Source); err != nil {
		t.Fatalf("migrate the kernel: %v", err)
	}
	admin := dbtest.Open(t, adminURL)
	owner := moduleOwner(t, admin, 'b')
	revision := strings.Join([]string{
		openSchema(owner),
		tenantTable(owner, "items", true),
		// The failure a real file can hit: a later statement in the same file
		// names something that is not there.
		"SELECT * FROM platformkit_revision_that_does_not_exist",
	}, "\n")

	err := db.Migrate(ctx, adminURL, migrations.Source, db.MigrationSource{
		Owner: owner,
		Files: fstest.MapFS{"000001_schema.up.sql": {Data: []byte(revision)}},
	})
	if err == nil {
		t.Fatal("a migration file that failed at its last statement was reported as applied")
	}

	var schemas int
	if err := admin.QueryRowContext(ctx,
		"SELECT count(*) FROM pg_namespace WHERE nspname = $1", owner).Scan(&schemas); err != nil {
		t.Fatalf("ask whether %q exists: %v", owner, err)
	}
	if schemas != 0 {
		t.Errorf("the failed revision left %d schemas named %q: a schema its module never committed "+
			"to is exactly the half-open schema 000026's header refuses to leave behind", schemas, owner)
	}
	var rows int
	if err := admin.QueryRowContext(ctx,
		"SELECT count(*) FROM schema_migrations WHERE owner = $1", owner).Scan(&rows); err != nil {
		t.Fatalf("count %q's history: %v", owner, err)
	}
	if rows != 0 {
		t.Errorf("the failed revision left %d ledger rows for %q, so the tenant-scope walk would treat "+
			"that name as an owner whose schema it has checked", rows, owner)
	}
}

// holds asks PostgreSQL's own catalog the operational question — may this role do
// this, to this table — rather than reading an ACL by hand.
func holds(t *testing.T, ctx context.Context, admin *sql.DB, role, table, priv string) bool {
	t.Helper()
	var got bool
	if err := admin.QueryRowContext(ctx,
		"SELECT pg_catalog.has_table_privilege($1, $2::regclass, $3)", role, table, priv).Scan(&got); err != nil {
		t.Fatalf("ask whether %q may %s %s: %v", role, priv, table, err)
	}
	return got
}
