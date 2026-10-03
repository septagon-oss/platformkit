package migrations_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Two claims migrations/000026_module_schema.up.sql makes and
// module_schema_test.go never reaches, so the shipped statements behind them
// can be deleted and `go test ./migrations/... -count=1` stays green:
//
//   * the function's third statement — `ALTER DEFAULT PRIVILEGES IN SCHEMA
//     <owner> GRANT USAGE, SELECT ON SEQUENCES TO <role>`, one of the three the
//     brief names — reaches no test at all, because no test creates a
//     sequence-backed column in an opened schema. Without it the application
//     role gets `permission denied for sequence` on the ordinary module table
//     `id serial`, which is the failure this function exists to prevent.
//   * `%I` around the schema name is justified in the header by the grammar the
//     ledger uses — `^[a-z][a-z0-9_-]*$` admits a hyphen, which an unquoted
//     identifier does not survive — and no test opens such an owner, so
//     `%I` could become `%s` in silence.

// TestModuleSchemaOpensTheOwnerSequencesToTheApplicationRole writes a row into
// a sequence-backed column of a module's table, as the application role, inside
// one tenant's transaction. It names no role: the sequence grant it checks is
// the one the function hands out by discovery.
func TestModuleSchemaOpensTheOwnerSequencesToTheApplicationRole(t *testing.T) {
	ctx := t.Context()
	admin, app := dbtest.Schema(t)
	owner := moduleOwner(t, admin, 's')
	if _, err := admin.ExecContext(ctx, "SELECT platformkit_module_schema($1)", owner); err != nil {
		t.Fatalf("open a schema owned as %q: %v", owner, err)
	}
	// The shape a module's second revision takes, with the one column that
	// needs a sequence. `id serial` is nextval('..._id_seq') as a column
	// default, so the inserting role needs USAGE on that sequence — which is
	// what the third statement of the grantee loop hands out.
	for _, statement := range []string{
		fmt.Sprintf("CREATE TABLE %s.counter (tenant_id uuid NOT NULL, id serial NOT NULL, name text NOT NULL, PRIMARY KEY (tenant_id, id))", owner),
		fmt.Sprintf("ALTER TABLE %s.counter ENABLE ROW LEVEL SECURITY", owner),
		fmt.Sprintf("ALTER TABLE %s.counter FORCE ROW LEVEL SECURITY", owner),
		fmt.Sprintf("CREATE POLICY counter_tenant ON %s.counter USING (platformkit_tenant_match(tenant_id)) WITH CHECK (platformkit_tenant_match(tenant_id))", owner),
	} {
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			t.Fatalf("a module's own revision: %s: %v", statement, err)
		}
	}

	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "acme"}
	if err := db.Run(tenancy.WithTenant(ctx, tenant), app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec("INSERT INTO "+owner+".counter (tenant_id, name) VALUES (?, ?)",
			tenant.ID, "acme-row").Error
	}); err != nil {
		t.Fatalf("the application role could not write a sequence-backed row of %s.counter: %v", owner, err)
	}
	var id int64
	if err := db.Run(tenancy.WithTenant(ctx, tenant), app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT max(id) FROM " + owner + ".counter").Row().Scan(&id)
	}); err != nil {
		t.Fatalf("read back %s.counter: %v", owner, err)
	}
	if id < 1 {
		t.Errorf("%s.counter took id %d from its sequence, want the first value it handed out", owner, id)
	}
}

// TestModuleSchemaOpensAnOwnerThatNeedsQuoting takes an owner the ledger's
// grammar admits and a bare SQL identifier does not: a hyphen after the first
// character. It is the reason the function builds its statements with %I, and
// the schema's own name is the assertion — an unquoted CREATE SCHEMA raises a
// syntax error before any grant, and the row the application role writes is the
// proof the whole path (schema, table, sequence-free) came through quoted.
func TestModuleSchemaOpensAnOwnerThatNeedsQuoting(t *testing.T) {
	ctx := t.Context()
	admin, app := dbtest.Schema(t)
	owner := quotingOwner(t, admin)
	if _, err := admin.ExecContext(ctx, "SELECT platformkit_module_schema($1)", owner); err != nil {
		t.Fatalf("open a schema whose name needs quoting as %q: %v", owner, err)
	}
	// The schema exists under the name the caller wrote, not a shortened or
	// folded one: pg_namespace is the only place that answer comes from.
	var named int
	if err := admin.QueryRowContext(ctx,
		"SELECT count(*) FROM pg_namespace WHERE nspname = $1", owner).Scan(&named); err != nil {
		t.Fatalf("ask whether %q exists: %v", owner, err)
	}
	if named != 1 {
		t.Fatalf("the schema named %q exists %d times, want the one this call created", owner, named)
	}
	for _, statement := range []string{
		fmt.Sprintf("CREATE TABLE %s.items (tenant_id uuid NOT NULL, id uuid NOT NULL, name text NOT NULL, PRIMARY KEY (tenant_id, id))", quoteIdentifier(owner)),
		fmt.Sprintf("ALTER TABLE %s.items ENABLE ROW LEVEL SECURITY", quoteIdentifier(owner)),
		fmt.Sprintf("ALTER TABLE %s.items FORCE ROW LEVEL SECURITY", quoteIdentifier(owner)),
		fmt.Sprintf("CREATE POLICY items_tenant ON %s.items USING (platformkit_tenant_match(tenant_id)) WITH CHECK (platformkit_tenant_match(tenant_id))", quoteIdentifier(owner)),
	} {
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			t.Fatalf("a module's own revision: %s: %v", statement, err)
		}
	}
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "acme"}
	if err := db.Run(tenancy.WithTenant(ctx, tenant), app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec("INSERT INTO "+quoteIdentifier(owner)+".items (tenant_id, id, name) VALUES (?, ?, ?)",
			tenant.ID, uuid.New(), "acme-row").Error
	}); err != nil {
		t.Fatalf("the application role could not write to the quoted schema %s: %v", owner, err)
	}
	if got := itemNames(t, tenancy.WithTenant(ctx, tenant), app, quoteIdentifier(owner)); got != "acme-row" {
		t.Errorf("acme reads %q from %s.items, want only its own row", got, owner)
	}
}

// quotingOwner is the owner this test alone holds, spelled the way the ledger's
// grammar allows and a bare identifier does not: dbtest.schemaName is
// t_<run>_<test>, and the second character becomes a hyphen, so the run id that
// keeps one test binary's schemas apart from another's survives and the length
// stays inside the 63 bytes PostgreSQL silently truncates to.
func quotingOwner(t *testing.T, admin *sql.DB) string {
	t.Helper()
	var schema string
	if err := admin.QueryRowContext(t.Context(), "SELECT current_setting('search_path')").Scan(&schema); err != nil {
		t.Fatalf("read this test's schema: %v", err)
	}
	if len(schema) < 3 || schema[0] != 't' || schema[1] != '_' {
		t.Fatalf("dbtest's schema %q no longer starts with t_, and this derivation is its shape", schema)
	}
	owner := "q-" + schema[2:]
	drop := context.WithoutCancel(t.Context())
	t.Cleanup(func() {
		if _, err := admin.ExecContext(drop, "DROP SCHEMA IF EXISTS "+quoteIdentifier(owner)+" CASCADE"); err != nil {
			t.Errorf("drop the module schema %q: %v", owner, err)
		}
	})
	return owner
}

// quoteIdentifier wraps a name in the double quotes Postgres requires for the
// hyphen the grammar admits. The names here come from dbtest.schemaName, which
// emits [a-z0-9_] only, so nothing can be injected through it.
func quoteIdentifier(name string) string { return `"` + name + `"` }
