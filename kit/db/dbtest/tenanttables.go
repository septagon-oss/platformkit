package dbtest

// TenantTablesSQL is the tenant-scope walk: every ordinary table a migration
// could have created, in every schema a module owns, for a caller to assert is
// scoped to a tenant or declared exempt.
//
// It is exported because the claim is one claim and two repositories make it.
// migrations/rls_test.go and the catalog's own application test ran byte-
// identical copies of the SELECT below, and both were pinned to
// current_schema(), which was the only schema until a module's SQL could own
// one. A table in a module's schema is a table that check never saw, and the
// way a walk goes wrong is by quietly returning fewer rows, not by failing.
//
// Six columns, in this order:
//
//  1. schema name        — n.nspname, the new column: a table is only
//     identifiable as cart.items, because under one
//     schema per owner two modules may each have an items.
//  2. table name         — c.relname. Report the two together, never the bare
//     name: "items has no row-level security" is not a
//     message anybody can act on when four schemas have an
//     items.
//  3. RLS enabled        — c.relrowsecurity, from ENABLE ROW LEVEL SECURITY.
//  4. RLS forced         — c.relforcerowsecurity, from FORCE. ENABLE alone
//     exempts the table's owner from its own policy, so a
//     caller has to check both.
//  5. table comment      — obj_description, ” when there is none: this is
//     where the exemption marker lives, and a walk that
//     did not return it could not tell deliberate from
//     forgotten.
//  6. policy expressions — the table's policies' USING expressions, from
//     pg_get_expr(polqual), joined with spaces, ” when
//     the table has none. A caller matches this against
//     platformkit_tenant_match(tenant_id).
//
// Which schemas. current_schema(), which is public in an application and the
// test's own schema under this package, and every schema whose name is an
// owner in schema_migrations — the ledger is what makes a schema a module's, so
// a schema created by raw SQL, or by a hand-written ledger row, is not one.
// That is also why the walk belongs to a caller running on the same URL as the
// DDL: schema_migrations resolves through the caller's search_path, and
// pg_get_expr deparses a policy's function call qualified or bare depending on
// whether that function's schema is on the path.
//
// Which URL. Run it on the admin URL this package returns — the first one URLs
// and URLsFor hand back, the owner of the schema — and not on the application
// URL beside it. The runner revokes the application role's rights on
// schema_migrations (kit/db/migrate.go, migrationLedger), because a role that
// could forge history could forge a migration; so the walk on that URL is
// refused for one table rather than quietly listing fewer schemas, which is the
// loud answer and the only acceptable one: a walk goes wrong by returning fewer
// rows, not by failing. The same revoke is what makes an application's own
// catalog test of this constant a permission error with no hint in it, which is
// why the owner connection is the one to copy into a caller.
const TenantTablesSQL = `
	SELECT n.nspname, c.relname, c.relrowsecurity, c.relforcerowsecurity,
		coalesce(obj_description(c.oid, 'pg_class'), ''),
		coalesce((SELECT string_agg(pg_get_expr(p.polqual, p.polrelid), ' ')
			FROM pg_policy p WHERE p.polrelid = c.oid), '')
	FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
	WHERE c.relkind = 'r'
		AND (n.nspname = current_schema()
			OR n.nspname IN (SELECT owner FROM schema_migrations))
	ORDER BY n.nspname, c.relname`
