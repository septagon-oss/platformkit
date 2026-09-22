package db_test

// review4_cross_tenant_drain_test.go is the fourth review's case for the one claim
// about the drain that no test in this repository has ever discriminated: that a
// phase=data file reaches *every* tenant's rows, and does it because of the
// tenancy setting `crossTenants` writes rather than because the role that runs a
// migration happens to be a superuser.
//
// kit/db/backfill.go states both halves:
//
//	crossTenants is the one place a migration reaches every tenant's rows …
//	one that simply ran as the table owner would be refused by the FORCE every
//	tenant table carries.
//
// and migrations/000001_tenancy.up.sql states the other half it depends on:
//
//	FORCE matters: without it the table owner escapes the policy.
//
// Every case here has run the drain as `postgres`, which is a superuser with
// BYPASSRLS, and PostgreSQL stops evaluating a policy for such a role whatever
// the table declares — so every one of them would pass with the set_config call
// deleted, and a drain that silently wrote one tenant's rows and marked the
// version applied would have gone unnoticed. This case runs the drain as what a
// hardened deployment actually hands a migration: a login role that is not a
// superuser, not BYPASSRLS, and the *owner* of the table being drained, so the
// FORCE is the only thing between it and the policy.
//
// The first half of the case is the reachability probe, and it is measured, not
// asserted: it asks the same role, with a tenant on its session, how many rows it
// can see. If that role could see every row on its own, this file would prove
// nothing and says so by failing.

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/migrations"
)

const (
	forcedTenantA = "11111111-1111-1111-1111-111111111111"
	forcedTenantB = "22222222-2222-2222-2222-222222222222"
)

// tenancyHelpers is the kernel's own SQL for platformkit_tenant_match and
// platformkit_is_system, read from the kernel's own source rather than copied:
// the claim under test is that the setting kit/db writes is the setting the
// policy reads, and a copy could drift out of step with the very thing it checks.
func tenancyHelpers(t *testing.T) []byte {
	t.Helper()
	text, err := fs.ReadFile(migrations.Source.Files, "000001_tenancy.up.sql")
	if err != nil {
		t.Fatalf("reading the kernel's tenancy file: %v", err)
	}
	return text
}

// forcedProbeTable is the tenant table, in exactly the shape migrations/000001 says
// every tenant table has: enabled, forced, and one policy on both sides.
const forcedProbeTable = `CREATE TABLE probe (
	id bigint PRIMARY KEY,
	tenant_id uuid NOT NULL,
	marked boolean NOT NULL DEFAULT false
);
ALTER TABLE probe ENABLE ROW LEVEL SECURITY;
ALTER TABLE probe FORCE ROW LEVEL SECURITY;
CREATE POLICY probe_tenant ON probe
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
INSERT INTO probe (id, tenant_id) SELECT g, '` + forcedTenantA + `'::uuid FROM generate_series(1, 7) g;
INSERT INTO probe (id, tenant_id) SELECT g, '` + forcedTenantB + `'::uuid FROM generate_series(101, 104) g;`

const forcedDrainBody = `-- pkit: phase=data
-- pkit: batch=3
-- pkit: table=probe
UPDATE probe SET marked = true WHERE id IN (SELECT id FROM batch)`

func TestADrainReachesEveryTenantOfATableItsOwnerRowSecurityBinds(t *testing.T) {
	adminURL, _ := dbtest.URLs(t)
	admin := dbtest.Open(t, adminURL)
	ctx := t.Context()

	source := func(extra fstest.MapFS) db.MigrationSource {
		files := fstest.MapFS{
			"000001_tenancy.up.sql": {Data: tenancyHelpers(t)},
			"000002_probe.up.sql":   {Data: []byte(forcedProbeTable)},
		}
		for name, file := range extra {
			files[name] = file
		}
		return db.MigrationSource{Owner: "forcedrain", Files: files}
	}
	if err := db.Migrate(ctx, adminURL, source(nil)); err != nil {
		t.Fatalf("migrating the tenant table: %v", err)
	}

	// The deployment's migrate role, hardened: a login that owns its tables and is
	// given nothing else. `postgres` cannot answer the question this case asks.
	schema := schemaOf(t, adminURL)
	owner := roleOwner(schema)
	const password = "review4-not-a-superuser"
	for _, statement := range []string{
		fmt.Sprintf(`CREATE ROLE %s LOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE PASSWORD '%s'`, quoteIdent(owner), password),
		fmt.Sprintf(`GRANT USAGE ON SCHEMA %s TO %s`, quoteIdent(schema), quoteIdent(owner)),
		fmt.Sprintf(`GRANT SELECT, INSERT, UPDATE, DELETE ON schema_migrations, schema_migration_backfill, probe TO %s`, quoteIdent(owner)),
		fmt.Sprintf(`ALTER TABLE probe OWNER TO %s`, quoteIdent(owner)),
	} {
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			t.Fatalf("setting up the owner role: %v", err)
		}
	}
	t.Cleanup(func() {
		clean, err := sql.Open("pgx", adminURL)
		if err != nil {
			return
		}
		defer clean.Close()
		drop := context.WithoutCancel(ctx)
		_, _ = clean.ExecContext(drop, "DROP OWNED BY "+quoteIdent(owner)+" CASCADE")
		_, _ = clean.ExecContext(drop, "DROP ROLE IF EXISTS "+quoteIdent(owner))
	})

	ownerURL := connectAs(t, adminURL, owner, password)

	// The probe, and the reason the case has teeth: this role owns the table, and
	// the FORCE still refuses it the other tenant's rows. A role that could read
	// everything on its own rights would let the drain pass with no tenancy
	// setting at all, and this file would be proof of nothing.
	if n := visibleRows(t, ownerURL, forcedTenantA); n != 7 {
		t.Fatalf("the owner role sees %d rows with tenant A on its session; FORCE ROW LEVEL SECURITY is not binding this role (7 expected), so this case proves nothing about the drain", n)
	}
	if n := visibleRows(t, ownerURL, ""); n != 0 {
		t.Fatalf("the owner role sees %d rows with no tenant on its session; the policy denies rather than leaks, and a role that sees rows outside a tenant transaction is not the role this claim is about", n)
	}

	// The drain, run as that role and nobody else's.
	pending := fstest.MapFS{"000003_mark.up.sql": {Data: []byte(forcedDrainBody)}}
	if err := db.Backfill(ctx, ownerURL, source(pending)); err != nil {
		t.Fatalf("the drain, as the role a hardened deployment migrates with: %v", err)
	}

	// What the claim is: every tenant's rows, not the one the runner's session
	// happens to be able to reach. Read back through the kernel's own system door
	// so the assertion is about the table, not about the role that wrote it.
	if n := markedRows(t, adminURL, forcedTenantB); n != 4 {
		t.Errorf("the drain wrote %d of tenant B's 4 rows while tenant A had %d of 7; a phase=data file that reaches only what its own session may read marks the version applied anyway, which is a backfill that silently did nothing",
			n, markedRows(t, adminURL, forcedTenantA))
	}
	if n := ledgerCount(t, adminURL, "SELECT count(*) FROM schema_migrations WHERE owner = 'forcedrain' AND version = 3"); n != 1 {
		t.Errorf("the drain wrote %d history rows for version 3", n)
	}
	if n := ledgerCount(t, adminURL, "SELECT count(*) FROM schema_migration_backfill WHERE owner = 'forcedrain'"); n != 0 {
		t.Errorf("%d progress row(s) outlived the drain", n)
	}
}

// visibleRows is what the owner role can read with one tenant on its session, or
// with none when tenant is empty.
func visibleRows(t *testing.T, ownerURL, tenant string) int {
	t.Helper()
	pool := dbtest.OpenFor(t, ownerURL)
	conn, err := pool.Conn(t.Context())
	if err != nil {
		t.Fatalf("as the owner role: %v", err)
	}
	defer conn.Close()
	tx, err := conn.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatalf("as the owner role: %v", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(t.Context(), "SELECT set_config('platformkit.tenant_id', $1, true)", tenant); err != nil {
		t.Fatalf("as the owner role: %v", err)
	}
	var n int
	if err := tx.QueryRowContext(t.Context(), "SELECT count(*) FROM probe").Scan(&n); err != nil {
		t.Fatalf("counting as the owner role: %v", err)
	}
	return n
}

// markedRows counts one tenant's drained rows from the schema owner, the one
// connection in this file that sees every row whatever the policy says. The drain
// did its work under the tenancy setting and the FORCE; this only counts what
// landed, so the assertion is about the table and not about the rights of whoever
// wrote it.
func markedRows(t *testing.T, adminURL, tenant string) int {
	t.Helper()
	return ledgerCount(t, adminURL, "SELECT count(*) FROM probe WHERE marked AND tenant_id = "+quoteLiteral(tenant))
}

func ledgerCount(t *testing.T, rawURL, query string) int {
	t.Helper()
	var n int
	if err := dbtest.OpenFor(t, rawURL).QueryRowContext(t.Context(), query).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// schemaOf reads the search_path dbtest put on the connection URL, which is the
// schema this test owns.
func schemaOf(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("%q is not a URL: %v", rawURL, err)
	}
	options := u.Query().Get("options")
	_, schema, found := strings.Cut(options, "-csearch_path=")
	if !found || schema == "" {
		t.Fatalf("%q carries no search_path: %q", rawURL, options)
	}
	return schema
}

// roleOwner is the migration role this case creates, named after the schema it
// owns so that two packages running this test do not create one role twice.
func roleOwner(schema string) string {
	if len(schema) > 58 {
		schema = schema[:58]
	}
	return schema + "_owner"
}

func connectAs(t *testing.T, rawURL, role, password string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("%q is not a URL: %v", rawURL, err)
	}
	u.User = url.UserPassword(role, password)
	return u.String()
}

func quoteIdent(id string) string { return `"` + strings.ReplaceAll(id, `"`, `""`) + `"` }
