package dbtest

// TenantTablesSQL is the tenant-scope walk: every ordinary table a migration could
// have created, in every schema a module owns, for a caller to assert is scoped to a
// tenant or declared exempt. It is exported because the claim is one claim and two
// repositories make it, and because a walk goes wrong by quietly returning fewer
// rows, not by failing.
//
// Six columns in order — schema, table, RLS enabled, RLS forced, the table comment
// (where the exemption marker lives) and the policy expressions — over
// current_schema() plus every schema whose name is an owner in schema_migrations.
// Run it on the admin URL this package returns: the runner revokes the application
// role's rights on the migration ledger. See README.md, "The tenant-scope walk".
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
