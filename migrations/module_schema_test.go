package migrations_test

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/migrations"
)

// The four claims migrations/000026_module_schema.up.sql and
// dbtest.TenantTablesSQL make, through the two roles the stack runs as: the
// owner that migrates and the application role that is bound by row-level
// security. Each test derives its own owner name, so no two of them — and none
// of them and the catalog's tests — ever share a schema; see moduleOwner.

// TestModuleSchemaOpensTheOwnerToTheApplicationRole is the whole reason the
// function exists: after one call as the migration role, a table created in the
// module's schema is readable and writable by the application role, with no
// grant in the module's SQL and no role named anywhere in it. Before it, every
// query against cart.items was 42501 permission denied, and the only fix
// available was for the module's SQL to name a deployment's role, which it may
// not do.
func TestModuleSchemaOpensTheOwnerToTheApplicationRole(t *testing.T) {
	ctx := t.Context()
	admin, app := dbtest.Schema(t)
	owner := moduleOwner(t, admin, 'm')
	if _, err := admin.ExecContext(ctx, "SELECT platformkit_module_schema($1)", owner); err != nil {
		t.Fatalf("open a schema owned as %q: %v", owner, err)
	}
	// This is the SQL a module's own next revisions would write — the shape
	// 000001 documents, in the schema the line above opened. There is no GRANT
	// in it, and the role that reads the table is not named in it either.
	for _, statement := range []string{
		fmt.Sprintf("CREATE TABLE %s.items (tenant_id uuid NOT NULL, id uuid NOT NULL, name text NOT NULL, PRIMARY KEY (tenant_id, id))", owner),
		fmt.Sprintf("ALTER TABLE %s.items ENABLE ROW LEVEL SECURITY", owner),
		fmt.Sprintf("ALTER TABLE %s.items FORCE ROW LEVEL SECURITY", owner),
		fmt.Sprintf("CREATE POLICY items_tenant ON %s.items USING (platformkit_tenant_match(tenant_id)) WITH CHECK (platformkit_tenant_match(tenant_id))", owner),
	} {
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			t.Fatalf("a module's own revision: %s: %v", statement, err)
		}
	}

	acme := tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "acme"}
	globex := tenancy.Tenant{ID: uuid.New(), Slug: "globex", Name: "globex"}
	for _, tenant := range []tenancy.Tenant{acme, globex} {
		if err := db.Run(tenancy.WithTenant(ctx, tenant), app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			return tx.DB().Exec("INSERT INTO "+owner+".items (tenant_id, id, name) VALUES (?, ?, ?)",
				tenant.ID, uuid.New(), tenant.Slug+"-row").Error
		}); err != nil {
			t.Fatalf("%s could not write to the module's table: %v", tenant.Slug, err)
		}
	}
	// Both halves at once: the grant is real, and it is not a leak. The table's
	// owner is the migration role and the application role is only a grantee,
	// which is what makes FORCE the thing that binds here (000001).
	for _, tenant := range []tenancy.Tenant{acme, globex} {
		// The read runs as this tenant, the way the write above did: db.Run
		// refuses a Tx[db.Tenant] whose context names no tenant, and "only its
		// own row" is a claim made from inside one tenant's transaction.
		if got := itemNames(t, tenancy.WithTenant(ctx, tenant), app, owner); got != tenant.Slug+"-row" {
			t.Errorf("%s reads %q from %s.items, want only its own row", tenant.Slug, got, owner)
		}
	}
}

// TestModuleSchemaRefusesEveryNameThatIsNotAModuleSchema. Each refusal carries
// the rule it applied in its own message: the caller is a migration file, and
// the exception text is all it gets. The classification is written beside each
// case — correctable means the same module can call again with a different
// name, immutable means no name the module may legitimately hold would pass.
func TestModuleSchemaRefusesEveryNameThatIsNotAModuleSchema(t *testing.T) {
	ctx := t.Context()
	admin, _ := dbtest.Schema(t)
	cases := []struct {
		owner string
		rule  string
		// already is how many schemas of that name the deployment has before
		// the call: the refusal must leave exactly that, no more.
		already int
	}{
		{"public", "is a namespace the deployment or the catalog owns", 1},             // immutable
		{"information_schema", "is a namespace the deployment or the catalog owns", 1}, // immutable
		{"pg_temp", "is a namespace the deployment or the catalog owns", 0},            // immutable
		{"Bad Owner", `must match ^[a-z][a-z0-9_-]*$`, 0},                              // correctable: name the owner as the ledger names it
		{"", `must match ^[a-z][a-z0-9_-]*$`, 0},                                       // immutable: no module has no name
		{strings.Repeat("m", 64), "silently truncated", 0},                             // immutable: longer than a Postgres identifier
	}
	for _, c := range cases {
		t.Run(c.owner, func(t *testing.T) {
			if _, err := admin.ExecContext(ctx, "SELECT platformkit_module_schema($1)", c.owner); err == nil {
				t.Fatalf("platformkit_module_schema(%q) was accepted, and it names a schema no module may own", c.owner)
			} else if !strings.Contains(err.Error(), c.rule) {
				t.Errorf("refusing %q said %q, which does not name the rule %q", c.owner, err, c.rule)
			}
			// A refusal writes nothing: no schema, and no half-open one.
			var created int
			if err := admin.QueryRowContext(ctx,
				"SELECT count(*) FROM pg_namespace WHERE nspname = $1", c.owner).Scan(&created); err != nil {
				t.Fatalf("ask whether %q exists: %v", c.owner, err)
			}
			if created != c.already {
				t.Errorf("refusing %q left %d schemas named it, want the %d that were already there", c.owner, created, c.already)
			}
		})
	}
	// No name at all, which is not the same case as the empty string: `NULL !~
	// pattern` is NULL, and a body that tested only the regex would walk past a
	// NULL owner into a CREATE SCHEMA whose name it did not have.
	if _, err := admin.ExecContext(ctx, "SELECT platformkit_module_schema($1)", nil); err == nil {
		t.Error("platformkit_module_schema(NULL) was accepted; a nameless schema belongs to no owner")
	} else if !strings.Contains(err.Error(), `must match ^[a-z][a-z0-9_-]*$`) {
		t.Errorf("refusing NULL said %q, which does not name the rule it applied", err)
	}
}

// TestModuleSchemaIsOneCallPerOwner: composing the same module twice, or
// running the same release twice against a database that already has the
// schema, must be silent. Nothing is revoked, nothing is duplicated, and the
// default privileges a second call would have added are the ones the first call
// already put there.
func TestModuleSchemaIsOneCallPerOwner(t *testing.T) {
	ctx := t.Context()
	admin, _ := dbtest.Schema(t)
	owner := moduleOwner(t, admin, 'c')
	open := func() {
		t.Helper()
		if _, err := admin.ExecContext(ctx, "SELECT platformkit_module_schema($1)", owner); err != nil {
			t.Fatalf("open a schema owned as %q: %v", owner, err)
		}
	}
	// Counted for this schema alone and never globally: tests share one
	// database, and every schema dbtest makes holds two default-privilege rows
	// of its own.
	defaults := func() int {
		t.Helper()
		var n int
		if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM pg_default_acl
			WHERE defaclnamespace = (SELECT oid FROM pg_namespace WHERE nspname = $1)`, owner).Scan(&n); err != nil {
			t.Fatalf("count %q's default privileges: %v", owner, err)
		}
		return n
	}
	schemas := func() int {
		t.Helper()
		var n int
		if err := admin.QueryRowContext(ctx,
			"SELECT count(*) FROM pg_namespace WHERE nspname = $1", owner).Scan(&n); err != nil {
			t.Fatalf("count the schemas named %q: %v", owner, err)
		}
		return n
	}

	open()
	if got := schemas(); got != 1 {
		t.Fatalf("one call left %d schemas named %q", got, owner)
	}
	first := defaults()
	if first == 0 {
		t.Fatalf("one call left no default privileges in %q: the deployment grants none by default, "+
			"and the schema it opened can be read by its owner alone", owner)
	}
	open()
	if got := schemas(); got != 1 {
		t.Errorf("a second call left %d schemas named %q, want 1", got, owner)
	}
	if got := defaults(); got != first {
		t.Errorf("a second call changed %q's default privileges from %d rows to %d", owner, first, got)
	}
}

// TestModuleSchemaHandsAGranteeJustThePrivilegesItsOwnRowNames narrows the
// deployment's defaults on the object kind nothing else narrows — a sequence —
// and on both kinds at once. The deployment is the ordinary shape: a reporting
// role that may read tables and read sequences, and may do neither of the two
// things beside them. Inside a schema this function opened, that answer has to
// still be the deployment's: row-level security binds which rows a role may see
// and says nothing about whether it may write one, so the privilege list the
// function read out of pg_default_acl is the only thing holding the line. The
// catalog questions are the operational ones — may this role read this table,
// may this role call nextval — and the effect side, one INSERT refused at a
// table, is the second review's pin beside this one.
func TestModuleSchemaHandsAGranteeJustThePrivilegesItsOwnRowNames(t *testing.T) {
	ctx := t.Context()
	admin, _ := dbtest.Schema(t)
	deployment := dbtest.DeploymentSchema(t, admin)
	// Same length as the schema it is cut from, the way moduleOwner changes a
	// letter rather than appending: Postgres shortens a longer name silently, and
	// the DROP ROLE below would then name a role that was never created.
	reader := "ro" + deployment[2:]
	drop := context.WithoutCancel(ctx)
	// Registered before moduleOwner's, so it runs after it: the schema the
	// function granted into is gone by the time this asks Postgres to drop a role
	// that still holds privileges inside it.
	t.Cleanup(func() {
		for _, statement := range []string{
			"ALTER DEFAULT PRIVILEGES IN SCHEMA " + deployment + " REVOKE ALL ON TABLES FROM " + reader,
			"ALTER DEFAULT PRIVILEGES IN SCHEMA " + deployment + " REVOKE ALL ON SEQUENCES FROM " + reader,
			"DROP ROLE IF EXISTS " + reader,
		} {
			if _, err := admin.ExecContext(drop, statement); err != nil {
				t.Errorf("cleanup %q: %v", reader, err)
			}
		}
	})
	owner := moduleOwner(t, admin, 'r')
	for _, statement := range []string{
		"CREATE ROLE " + reader + " NOSUPERUSER NOBYPASSRLS",
		// The narrow pair, and the narrow sequence in particular: a deployment
		// that pins SELECT alone is what makes "USAGE, SELECT" written into the
		// function rather than read out of the row visible here and nowhere else
		// in this suite.
		"ALTER DEFAULT PRIVILEGES IN SCHEMA " + deployment + " GRANT SELECT ON TABLES TO " + reader,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA " + deployment + " GRANT SELECT ON SEQUENCES TO " + reader,
	} {
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			t.Fatalf("stand up the deployment's reporting role: %s: %v", statement, err)
		}
	}
	if _, err := admin.ExecContext(ctx, "SELECT platformkit_module_schema($1)", owner); err != nil {
		t.Fatalf("open a schema owned as %q: %v", owner, err)
	}
	// The module's own next revision, sequence and all. Names come from
	// dbtest.DeploymentSchema, and this package names its schema with [a-z0-9_]
	// only, so nothing here can be injected.
	for _, statement := range []string{
		fmt.Sprintf("CREATE TABLE %s.counter (tenant_id uuid NOT NULL, id serial NOT NULL, PRIMARY KEY (tenant_id, id))", owner),
		fmt.Sprintf("ALTER TABLE %s.counter ENABLE ROW LEVEL SECURITY", owner),
		fmt.Sprintf("ALTER TABLE %s.counter FORCE ROW LEVEL SECURITY", owner),
		fmt.Sprintf("CREATE POLICY counter_tenant ON %s.counter USING (platformkit_tenant_match(tenant_id)) WITH CHECK (platformkit_tenant_match(tenant_id))", owner),
	} {
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			t.Fatalf("a module's own revision: %s: %v", statement, err)
		}
	}

	cases := []struct {
		ask  string
		want bool
	}{
		{fmt.Sprintf("has_table_privilege('%s', '%s.counter', 'SELECT')", reader, owner), true},
		{fmt.Sprintf("has_table_privilege('%s', '%s.counter', 'INSERT')", reader, owner), false},
		{fmt.Sprintf("has_sequence_privilege('%s', '%s.counter_id_seq', 'SELECT')", reader, owner), true},
		{fmt.Sprintf("has_sequence_privilege('%s', '%s.counter_id_seq', 'USAGE')", reader, owner), false},
	}
	for _, c := range cases {
		var got bool
		if err := admin.QueryRowContext(ctx, "SELECT "+c.ask).Scan(&got); err != nil {
			t.Fatalf("%s: %v", c.ask, err)
		}
		if got != c.want {
			t.Errorf("%s answered %t, want %t: the deployment pinned SELECT on tables and SELECT on "+
				"sequences, and the module's schema is meant to hand that role exactly that",
				c.ask, got, c.want)
		}
	}
}

// TestModuleSchemaMirrorsNoDatabaseWideDefault pins the half of the namespace
// rule no other test reaches: a role the deployment pinned its defaults to with
// no IN SCHEMA clause at all. Such a row (defaclnamespace = 0) says nothing
// about the namespace the migration runs in, so the loop finds no grantee in it
// and writes nothing into the module's schema — and the module's own table still
// hands that role its SELECT, because a database-wide row applies to every
// object the role creates in every schema, which is exactly why mirroring it
// here would have bought nothing. What it does not do is open a namespace, so
// the first read of a module's table by that role is refused for the schema:
// migrations/README.md's consequence, measured rather than asserted.
//
// The pin is FOR ROLE a migration role this test stood up, and that is a
// containment measure rather than a style. A database-wide pin made by the admin
// hands its privileges to every table any other test in this shared database
// creates while it stands — and PostgreSQL then refuses to drop the grantee, so
// the cleanup would fail on a table this test never touched. Pinning the
// defaults of a role that creates nothing but this test's tables keeps the
// widening where the claim is.
func TestModuleSchemaMirrorsNoDatabaseWideDefault(t *testing.T) {
	_, appURL, admin := kernelSchema(t)
	ctx := t.Context()
	deployment := deploymentSchema(t, ctx, admin)
	reader := "b" + deployment[2:]
	migrator := "d" + deployment[1:]
	owner := moduleOwner(t, admin, 'g')
	var database string
	if err := admin.QueryRowContext(ctx, "SELECT current_database()").Scan(&database); err != nil {
		t.Fatalf("read the database name: %v", err)
	}
	// Registered after moduleOwner's, so it runs before it: the reader holds a
	// grant on the module's table, and PostgreSQL refuses to drop a role that
	// still holds one, so the schema that table lives in goes first.
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, statement := range []string{
			"ALTER DEFAULT PRIVILEGES FOR ROLE " + migrator + " REVOKE ALL ON TABLES FROM " + reader,
			"DROP SCHEMA IF EXISTS " + owner + " CASCADE",
			"REVOKE ALL ON DATABASE " + database + " FROM " + migrator,
			"REVOKE ALL ON SCHEMA " + deployment + " FROM " + migrator,
			"DROP ROLE IF EXISTS " + reader,
			"DROP ROLE IF EXISTS " + migrator,
		} {
			if _, err := admin.ExecContext(cleanup, statement); err != nil {
				t.Errorf("cleanup %q: %v", statement, err)
			}
		}
	})

	for _, statement := range []string{
		"CREATE ROLE " + reader + " LOGIN PASSWORD 'platformkit' NOSUPERUSER NOBYPASSRLS",
		"CREATE ROLE " + migrator + " LOGIN PASSWORD 'platformkit' NOSUPERUSER NOBYPASSRLS",
		// What README.md says a dedicated migration role needs, and no defaults.
		"GRANT CREATE ON DATABASE " + database + " TO " + migrator,
		"GRANT USAGE ON SCHEMA " + deployment + " TO " + migrator,
		// The whole of the deployment's pinning: one row, and it names no schema.
		"ALTER DEFAULT PRIVILEGES FOR ROLE " + migrator + " GRANT SELECT ON TABLES TO " + reader,
	} {
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			t.Fatalf("stand up a deployment that pins database-wide: %s: %v", statement, err)
		}
	}

	migration := dbtest.Open(t, roleURL(t, appURL, migrator))
	if _, err := migration.ExecContext(ctx, "SELECT platformkit_module_schema($1)", owner); err != nil {
		t.Fatalf("open a schema as the role whose only defaults are database-wide: %v", err)
	}
	execRevision(t, migration, tenantTable(owner, "items", true))

	var mirrored int
	if err := admin.QueryRowContext(ctx,
		"SELECT count(*) FROM pg_default_acl WHERE defaclnamespace = to_regnamespace($1)", owner).Scan(&mirrored); err != nil {
		t.Fatalf("count %q's default privileges: %v", owner, err)
	}
	if mirrored != 0 {
		t.Errorf("the function mirrored %d default-privilege rows into %q from a pin that named no "+
			"namespace; those rows apply to a module's tables already, which is why writing them here is "+
			"how a privilege arrives in a schema the deployment never pinned it for", mirrored, owner)
	}
	ask := func(claim string) bool {
		t.Helper()
		var got bool
		if err := admin.QueryRowContext(ctx, "SELECT "+claim).Scan(&got); err != nil {
			t.Fatalf("%s: %v", claim, err)
		}
		return got
	}
	opened := ask(fmt.Sprintf("has_schema_privilege('%s', '%s', 'USAGE')", reader, owner))
	readable := ask(fmt.Sprintf("has_table_privilege('%s', '%s.items', 'SELECT')", reader, owner))
	if opened {
		t.Errorf("has_schema_privilege opened %q to %q, a grantee the namespace's own rows never named: "+
			"the deployment pinned table privileges database-wide and that is not a namespace", owner, reader)
	}
	if !readable {
		t.Errorf("%s.items does not carry the SELECT the deployment pinned database-wide for %q, so the "+
			"namespace filter removed a privilege the database-wide row already handed that role", owner, reader)
	}

	// The consequence, at the door a module's query travels: refused, loudly, and
	// for the schema rather than the table.
	app := openAsReader(t, ctx, appURL, reader)
	err := db.Run(tenancy.WithTenant(ctx, tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "acme"}), app,
		func(_ context.Context, tx db.Tx[db.Tenant]) error {
			return tx.DB().Raw("SELECT count(*) FROM " + owner + ".items").Row().Scan(new(int64))
		})
	if err == nil {
		t.Errorf("%q read %s.items although the namespace it runs in named it no grantee", reader, owner)
	} else if !strings.Contains(err.Error(), "permission denied for schema") {
		t.Errorf("the module's table refused the database-wide role for another reason, so this did not "+
			"measure the schema grant: %v", err)
	}
}

// TestTenantTablesWalkSeesEveryOwnerSchema is the second half of the claim: a
// module's tables are inside the tenant-scope walk, and the walk's caller can
// tell "this table is unprotected" from "this table was never looked at".
//
// Both schemas arrive through a migration source, because the ledger is what
// makes a schema a module's: the walk matches a schema against the owners in
// schema_migrations, and only db.Migrate writes that table (ADR 0011). The
// fixture therefore is the shape migrations/000026's header documents — one
// revision that opens the schema, the tables after it — and the second owner
// carries the mistake the walk exists to catch, ENABLE without FORCE.
func TestTenantTablesWalkSeesEveryOwnerSchema(t *testing.T) {
	adminURL, _ := dbtest.URLs(t)
	admin := dbtest.Open(t, adminURL)
	scoped := moduleOwner(t, admin, 'm')
	unforced := moduleOwner(t, admin, 'n')
	sources := []db.MigrationSource{
		migrations.Source,
		{Owner: scoped, Files: fstest.MapFS{
			"000001_schema.up.sql": {Data: []byte(openSchema(scoped))},
			"000002_items.up.sql":  {Data: []byte(tenantTable(scoped, "items", true))},
		}},
		{Owner: unforced, Files: fstest.MapFS{
			"000001_schema.up.sql":   {Data: []byte(openSchema(unforced))},
			"000002_unforced.up.sql": {Data: []byte(tenantTable(unforced, "unforced", false))},
		}},
	}
	if err := db.Migrate(t.Context(), adminURL, sources...); err != nil {
		t.Fatalf("migrate the kernel and two module owners: %v", err)
	}

	tables, problems := tenantScope(t, admin)
	if want := scoped + ".items"; !slices.Contains(tables, want) {
		t.Errorf("the walk did not list %s, although the ledger says %q is an owner; it listed %v",
			want, scoped, tables)
	} else if slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, want) }) {
		t.Errorf("the walk reported the table that carries the tenant policy: %v", problems)
	}
	// The kernel's own tables stay in the walk: dropping them with the second
	// arm added would leave the older half of the claim unchecked.
	if !slices.ContainsFunc(tables, func(table string) bool { return strings.HasSuffix(table, ".platformkit_limits") }) {
		t.Errorf("the walk no longer lists the kernel's own tables; it listed %v", tables)
	}
	// The mistake, in a module's schema: reported, and named where it is.
	want := unforced + ".unforced"
	if !slices.Contains(tables, want) {
		t.Fatalf("the walk did not list %s, so it cannot have reported it; it listed %v", want, tables)
	}
	if !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, want) }) {
		t.Errorf("a table in a module's schema is ENABLE without FORCE and the walk's caller said nothing about %s: %v",
			want, problems)
	}
}

// moduleOwner is the owner — and so, by ADR 0024's R1, the schema — this test
// alone holds. It is the deployment's own schema name (dbtest.DeploymentSchema,
// read back from Postgres rather than rebuilt from the connection URL) with its
// first letter changed, which keeps it inside the 63 bytes Postgres truncates an
// identifier to: appending a suffix instead can silently shorten the name, and
// the ledger would then hold a name no schema has, which the walk would answer
// by finding nothing. It is dropped when the test ends, because dbtest cleans up
// only the schema it made.
func moduleOwner(t *testing.T, admin *sql.DB, marker rune) string {
	t.Helper()
	owner := string(marker) + dbtest.DeploymentSchema(t, admin)[1:]
	drop := context.WithoutCancel(t.Context())
	t.Cleanup(func() {
		if _, err := admin.ExecContext(drop, "DROP SCHEMA IF EXISTS "+owner+" CASCADE"); err != nil {
			t.Errorf("drop the module schema %q: %v", owner, err)
		}
	})
	return owner
}

// openSchema is a module's first schema revision: the one line 000026's header
// documents. The name is a literal here because a migration file names its own
// schema — and because moduleOwner derives it from dbtest.schemaName, which
// emits [a-z0-9_] only, so nothing can be injected into it.
func openSchema(owner string) string {
	return "SELECT platformkit_module_schema('" + owner + "');"
}

// tenantTable is the table shape migrations/000001 documents, in a module's own
// schema. force=false is the mistake the walk exists to catch, so it is written
// as one argument rather than as a second fixture nobody compares to the first.
func tenantTable(owner, name string, force bool) string {
	statements := fmt.Sprintf("CREATE TABLE %s.%s (tenant_id uuid NOT NULL, id uuid NOT NULL, PRIMARY KEY (tenant_id, id));\n"+
		"ALTER TABLE %s.%s ENABLE ROW LEVEL SECURITY;\n", owner, name, owner, name)
	if force {
		statements += fmt.Sprintf("ALTER TABLE %s.%s FORCE ROW LEVEL SECURITY;\n", owner, name)
	}
	return statements + fmt.Sprintf("CREATE POLICY %s_tenant ON %s.%s USING (platformkit_tenant_match(tenant_id)) WITH CHECK (platformkit_tenant_match(tenant_id));",
		name, owner, name)
}

func itemNames(t *testing.T, ctx context.Context, app *db.Conn, owner string) string {
	t.Helper()
	var names string
	if err := db.Run(ctx, app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT coalesce(string_agg(name, ',' ORDER BY name), '') FROM " + owner + ".items").Row().Scan(&names)
	}); err != nil {
		t.Fatalf("read %s.items: %v", owner, err)
	}
	return names
}
