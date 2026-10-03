package migrations_test

import (
	"context"
	"database/sql"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/migrations"
)

// The second review's pins for migrations/000026_module_schema.up.sql. Each test
// here takes a claim the function's header, migrations/README.md or the
// implementation report makes that no shipped test reaches — the same shape as
// the first review's finding, found the same way: ask what stays green if the
// statement is wrong, then run the case.
//
// TestModuleSchemaGrantsNoMoreThanTheDeploymentAlreadyHandedOut FAILS as filed.
// It asserts the guarantee both documents state — that the module schema hands a
// grantee "the same table and sequence default privileges the migration role
// already hands out" — against a deployment that hands one of its roles only
// SELECT. The function hands that role SELECT, INSERT, UPDATE and DELETE inside
// every schema it opens, which is a write the deployment refused outside it. It
// passes when the grantee loop grants the privilege types the grantee actually
// holds — aclexplode names each one in the row it already reads — instead of the
// widest set, or when the documents stop promising "the same" and a test pins a
// deliberate wider grant.
//
// The other three pass, and pin branches the implementation report records as
// unreachable from this suite ("a test for either needs the superuser door
// T-0029 is settling"): a schema another role owns, a grantee the catalog spells
// 0, and a deployment that pins no defaults at all. The admin URL dbtest hands
// every test is already SUPERUSER — kit/db/pool_test.go has created probe roles
// through it since — so all three are reachable today, with no new door.

// TestModuleSchemaGrantsNoMoreThanTheDeploymentAlreadyHandedOut is the
// escalation case. A deployment adds a reporting role and pins it read-only, the
// ordinary shape, and the one the header invites when it says a deployment "that
// pins its defaults to public, to the whole database, or to both, is found". The
// control runs first and is load-bearing: it asserts the deployment's own answer,
// so the probe cannot pass because the pin never took.
func TestModuleSchemaGrantsNoMoreThanTheDeploymentAlreadyHandedOut(t *testing.T) {
	_, appURL, admin := kernelSchema(t)
	ctx := t.Context()
	deployment := deploymentSchema(t, ctx, admin)
	owner := moduleOwner(t, admin, 'w')
	// Same length as the schema it is derived from, the way moduleOwner changes a
	// letter rather than appending: PostgreSQL shortens a longer name silently,
	// and the DROP ROLE below would then name a role that was never created.
	reader := "rw" + deployment[2:]
	dropRole(t, admin, reader,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA "+deployment+" REVOKE ALL ON TABLES FROM "+reader,
		"DROP SCHEMA IF EXISTS "+owner+" CASCADE",
		"REVOKE ALL ON ALL TABLES IN SCHEMA "+deployment+" FROM "+reader,
		"REVOKE ALL ON SCHEMA "+deployment+" FROM "+reader)

	// The deployment's own two statements, narrowed: USAGE on the namespace, and
	// SELECT on whatever it hands out by default. apps/platformkit/
	// postgres-init.sql writes that pair for platformkit_app with arwd; the
	// reporting role differs only in what it may do with a row.
	for _, statement := range []string{
		"CREATE ROLE " + reader + " LOGIN PASSWORD 'platformkit' NOSUPERUSER NOBYPASSRLS",
		"GRANT USAGE ON SCHEMA " + deployment + " TO " + reader,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA " + deployment + " GRANT SELECT ON TABLES TO " + reader,
	} {
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			t.Fatalf("stand up the deployment's read-only role: %s: %v", statement, err)
		}
	}
	// A tenant table in the deployment's own namespace, created after that pin,
	// and one in the schema the function opened: the same shape both times.
	execRevision(t, admin, tenantTable(deployment, "guard", true))
	if _, err := admin.ExecContext(ctx, "SELECT platformkit_module_schema($1)", owner); err != nil {
		t.Fatalf("open a schema owned as %q: %v", owner, err)
	}
	execRevision(t, admin, tenantTable(owner, "items", true))

	app := openAsReader(t, ctx, appURL, reader)
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "acme"}

	control := insertRow(t, ctx, app, tenant, deployment+".guard")
	switch {
	case control == nil:
		t.Fatal("the premise is broken: the read-only role wrote the deployment's own table, " +
			"so the narrow default privilege was never in place and the probe below would prove nothing")
	case !strings.Contains(control.Error(), "permission denied for table guard"):
		t.Fatalf("the deployment's own namespace refused the read-only role for another reason: %v", control)
	}

	// The probe: the same role, the same statement, the module's schema. The
	// message it must carry names the table, so a refusal from anywhere else in
	// the transaction cannot stand in for the privilege check under test.
	probe := insertRow(t, ctx, app, tenant, owner+".items")
	if probe == nil {
		t.Errorf("platformkit_module_schema widened the read-only role %q to a writer: it inserted into "+
			"%s.items, where the deployment's own namespace refused the same statement with %q. The grantee "+
			"loop hands every grantee SELECT, INSERT, UPDATE, DELETE whatever that grantee holds by default, "+
			"while README.md and 000026's header both call that \"the same\" privileges.",
			reader, owner, control)
	} else if !strings.Contains(probe.Error(), "permission denied for table items") {
		t.Errorf("the module schema refused the read-only role for another reason, so the privilege "+
			"grant is not what this test measured: %v", probe)
	}
}

// TestModuleSchemaRefusesASchemaAnotherRoleOwns reaches the refusal
// module_schema_test.go never touches and the implementation report leaves under
// "Not verified" because no second owning role exists in the reference stack. One
// GRANT creates it. The claim is the header's: the function raises rather than
// half-granting into somebody else's namespace.
func TestModuleSchemaRefusesASchemaAnotherRoleOwns(t *testing.T) {
	_, _, admin := kernelSchema(t)
	ctx := t.Context()
	deployment := deploymentSchema(t, ctx, admin)
	owner := moduleOwner(t, admin, 'f')
	foreign := "o" + deployment[1:]
	dropRole(t, admin, foreign, "DROP SCHEMA IF EXISTS "+owner+" CASCADE")

	for _, statement := range []string{
		"CREATE ROLE " + foreign + " NOSUPERUSER NOBYPASSRLS",
		"CREATE SCHEMA " + owner + " AUTHORIZATION " + foreign,
	} {
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}

	if _, err := admin.ExecContext(ctx, "SELECT platformkit_module_schema($1)", owner); err == nil {
		t.Fatal("platformkit_module_schema opened a schema another role owns, the half-granted " +
			"failure its header says it exists to prevent")
	} else if !strings.Contains(err.Error(), "owned by another role") {
		t.Errorf("refusing a foreign schema said %q, which does not name the rule it applied", err)
	}
	// A refusal that wrote nothing: no privilege for anyone but the owner, and no
	// default privileges for the schema.
	var granted int
	if err := admin.QueryRowContext(ctx, `
		SELECT count(*) FROM pg_namespace n, aclexplode(n.nspacl) a
		WHERE n.nspname = $1 AND a.grantee <> n.nspowner`, owner).Scan(&granted); err != nil {
		t.Fatalf("read %q's ACL: %v", owner, err)
	}
	if granted != 0 {
		t.Errorf("refusing the foreign schema %q still handed privileges to %d other roles", owner, granted)
	}
	var defaults int
	if err := admin.QueryRowContext(ctx,
		"SELECT count(*) FROM pg_default_acl WHERE defaclnamespace = to_regnamespace($1)", owner).Scan(&defaults); err != nil {
		t.Fatalf("count %q's default privileges: %v", owner, err)
	}
	if defaults != 0 {
		t.Errorf("refusing the foreign schema %q still wrote %d default privilege rows into it", owner, defaults)
	}
}

// TestModuleSchemaOpensNothingToAnyoneWithoutPinnedDefaults is the consequence
// migrations/README.md states beside the function's name: "a deployment whose
// migration role holds no default privileges yields zero grantees, and the schema
// the function opens is then readable by its owner alone". The reference stack
// cannot show it, because there the migration role is the role that pinned the
// defaults. This runs the call as a third role holding CREATE on the database and
// no default privileges at all, and asserts both halves of the sentence: the call
// succeeds, and the application role still cannot open the schema.
func TestModuleSchemaOpensNothingToAnyoneWithoutPinnedDefaults(t *testing.T) {
	_, appURL, admin := kernelSchema(t)
	ctx := t.Context()
	deployment := deploymentSchema(t, ctx, admin)
	owner := moduleOwner(t, admin, 'q')
	migrator := "m" + deployment[1:]

	var database string
	if err := admin.QueryRowContext(ctx, "SELECT current_database()").Scan(&database); err != nil {
		t.Fatalf("read the database name: %v", err)
	}
	dropRole(t, admin, migrator,
		"REVOKE ALL ON DATABASE "+database+" FROM "+migrator,
		"REVOKE ALL ON SCHEMA "+deployment+" FROM "+migrator,
		"DROP SCHEMA IF EXISTS "+owner+" CASCADE")
	for _, statement := range []string{
		"CREATE ROLE " + migrator + " LOGIN PASSWORD 'platformkit' NOSUPERUSER NOBYPASSRLS",
		// What README.md says a dedicated migration role has to be granted.
		"GRANT CREATE ON DATABASE " + database + " TO " + migrator,
		// And so it can reach the kernel's SQL, where the function lives.
		"GRANT USAGE ON SCHEMA " + deployment + " TO " + migrator,
	} {
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}

	migration := dbtest.Open(t, roleURL(t, appURL, migrator))
	if _, err := migration.ExecContext(ctx, "SELECT platformkit_module_schema($1)", owner); err != nil {
		t.Fatalf("a migration role holding no default privileges was refused: %v", err)
	}
	var readable bool
	if err := admin.QueryRowContext(ctx,
		"SELECT has_schema_privilege('platformkit_app', $1, 'USAGE')", owner).Scan(&readable); err != nil {
		t.Fatalf("ask whether the application role can open %q: %v", owner, err)
	}
	if readable {
		t.Errorf("the deployment pinned no default privileges and the schema %q was opened to the "+
			"application role anyway, so README.md's consequence describes a case the function does not have", owner)
	}
}

// TestModuleSchemaDiscoversPublicAsAGrantee runs the branch the header argues for
// at length and the reference stack never takes: aclexplode names the PUBLIC
// grantee as 0, which pg_get_userbyid renders as "unknown (OID=0)" and
// 0::oid::regrole as "-", either of which would reach EXECUTE as a syntax error.
// No shipped test executes that CASE, so the decode was protected by prose.
func TestModuleSchemaDiscoversPublicAsAGrantee(t *testing.T) {
	_, _, admin := kernelSchema(t)
	ctx := t.Context()
	deployment := deploymentSchema(t, ctx, admin)
	owner := moduleOwner(t, admin, 'p')

	if _, err := admin.ExecContext(ctx,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA "+deployment+" GRANT SELECT ON TABLES TO PUBLIC"); err != nil {
		t.Fatalf("pin a default privilege to PUBLIC, as a deployment may: %v", err)
	}
	if _, err := admin.ExecContext(ctx, "SELECT platformkit_module_schema($1)", owner); err != nil {
		t.Fatalf("open a schema on a deployment whose defaults go to PUBLIC: %v", err)
	}
	var public int
	if err := admin.QueryRowContext(ctx, `
		SELECT count(*) FROM pg_namespace n, aclexplode(n.nspacl) a
		WHERE n.nspname = $1 AND a.grantee = 0 AND a.privilege_type = 'USAGE'`, owner).Scan(&public); err != nil {
		t.Fatalf("read %q's ACL: %v", owner, err)
	}
	if public != 1 {
		t.Errorf("the grantee the catalog spells 0 did not reach the schema as PUBLIC: %q holds %d PUBLIC "+
			"USAGE entries, want 1", owner, public)
	}
}

// kernelSchema is a test's own schema carrying the kernel's tables and functions,
// which is where platformkit_module_schema lives: under dbtest the kernel's SQL
// lands in the test's schema, the way it lands in public in an installation.
func kernelSchema(t *testing.T) (adminURL, appURL string, admin *sql.DB) {
	t.Helper()
	adminURL, appURL = dbtest.URLs(t)
	if err := db.Migrate(t.Context(), adminURL, migrations.Source); err != nil {
		t.Fatalf("migrate the kernel: %v", err)
	}
	return adminURL, appURL, dbtest.Open(t, adminURL)
}

// deploymentSchema is this test's own schema, which is the deployment's `public`
// for everything in this file. It is the third copy of the read moduleOwner and
// quotingOwner make; those two are unexported helpers of this package, so a
// reviewer's file cannot call them.
func deploymentSchema(t *testing.T, ctx context.Context, admin *sql.DB) string {
	t.Helper()
	var schema string
	if err := admin.QueryRowContext(ctx, "SELECT current_setting('search_path')").Scan(&schema); err != nil {
		t.Fatalf("read this test's schema: %v", err)
	}
	if len(schema) < 3 || schema[0] != 't' || schema[1] != '_' {
		t.Fatalf("dbtest's schema %q no longer starts with t_, and every derivation here is its shape", schema)
	}
	return schema
}

// execRevision runs the statements tenantTable writes, one at a time, the way the
// shipped tests run a module's own revision.
func execRevision(t *testing.T, admin *sql.DB, revision string) {
	t.Helper()
	for _, statement := range strings.Split(revision, ";\n") {
		if _, err := admin.ExecContext(t.Context(), statement); err != nil {
			t.Fatalf("a module's own revision: %s: %v", statement, err)
		}
	}
}

// roleURL is the application URL with a different login role in it. The password
// is the one the reference stack ships, as kit/db/pool_test.go uses it; the
// search_path the URL already carries travels with it.
func roleURL(t *testing.T, appURL, role string) string {
	t.Helper()
	u, err := url.Parse(appURL)
	if err != nil {
		t.Fatalf("parse the application URL: %v", err)
	}
	u.User = url.UserPassword(role, "platformkit")
	return u.String()
}

// openAsReader is the deployment's read role through the application's own door,
// so the statement under test travels the path a module's query would.
func openAsReader(t *testing.T, ctx context.Context, appURL, role string) *db.Conn {
	t.Helper()
	app, err := db.Open(ctx, roleURL(t, appURL, role))
	if err != nil {
		t.Fatalf("open as the read-only role %q: %v", role, err)
	}
	t.Cleanup(func() { _ = app.Close() })
	return app
}

// insertRow is one tenant's write, inside that tenant's transaction the way
// db.Run hands one to a command, into the named table.
func insertRow(t *testing.T, ctx context.Context, app *db.Conn, tenant tenancy.Tenant, table string) error {
	t.Helper()
	return db.Run(tenancy.WithTenant(ctx, tenant), app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec("INSERT INTO "+table+" (tenant_id, id) VALUES (?, ?)", tenant.ID, uuid.New()).Error
	})
}

// dropRole ends a probe role's life. Cluster-global names are dropped with a
// context of their own, because a test that fails on an assertion still arrives
// here through t.Cleanup with its context already done. `before` runs first and
// holds whatever the role was granted: Postgres refuses to drop a role that still
// holds a privilege on a shared object — "privileges for schema public" is a
// dependency it will not drop implicitly — and refuses one that owns anything.
func dropRole(t *testing.T, admin *sql.DB, role string, before ...string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		statements := append(append([]string{}, before...), "DROP ROLE IF EXISTS "+role)
		for _, statement := range statements {
			if _, err := admin.ExecContext(ctx, statement); err != nil {
				t.Errorf("cleanup %q: %v", statement, err)
			}
		}
	})
}
