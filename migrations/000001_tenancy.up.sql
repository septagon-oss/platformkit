-- The tenancy helpers, declared once for the whole database.
--
-- Two transaction-local settings drive them, and only kit/db writes them:
--   platformkit.tenant_id     set by db.Run       (SELECT set_config(..., true))
--   platformkit.system_access set by db.RunSystem (SELECT set_config(..., true))
-- Both are local to the transaction, so they cannot leak across a pooled
-- connection. They are placeholder GUCs, which Postgres classes USERSET: no
-- privilege stops a statement rewriting one. What keeps them honest is
-- scripts/check_gucs.sh, which fails the build when any .go file outside kit/db
-- writes them, and db.Run/db.RunSystem, which re-read both before they commit.
--
-- A tenant-owned table protects itself with exactly this shape:
--
--   ALTER TABLE thing ENABLE ROW LEVEL SECURITY;
--   ALTER TABLE thing FORCE ROW LEVEL SECURITY;
--   CREATE POLICY thing_tenant ON thing
--     USING (platformkit_tenant_match(tenant_id))
--     WITH CHECK (platformkit_tenant_match(tenant_id));
--
-- FORCE matters: without it the table owner escapes the policy, and the
-- application role owns any table it creates itself. There are no sentinel
-- tenant ids and no exemptions; a row with no matching setting is simply not
-- visible. See TestForceRowLevelSecurityIsWhatBindsTheOwner.
--
-- All three functions are PARALLEL SAFE. They read only transaction settings,
-- so they are safe by their own semantics, and the label is what makes them
-- usable: a policy predicate that is not parallel safe makes the whole query
-- unparallelisable, so without it no tenant table ever gets a parallel plan.
--
-- None of them sets a search_path, on purpose. A SET clause blocks inlining,
-- which is the entire point of writing them as `LANGUAGE sql` one-liners: a
-- policy applies them once per row. The usual argument for pinning the path
-- does not apply here — Postgres never searches pg_temp for a function, and the
-- application role has no CREATE right on public — so the pin would cost the
-- inline and buy nothing.

-- Returns the tenant of the current transaction, or NULL outside one.
-- The CASE guard is load-bearing: current_setting returns whatever text was
-- placed, and an unguarded cast would raise on a malformed value instead of
-- failing closed. A plpgsql exception handler would do the same job but opens a
-- subtransaction on every row a policy checks; this stays inlinable SQL.
-- The pattern accepts only the canonical 8-4-4-4-12 form, which is narrower
-- than uuid_in: the sole writer is uuid.UUID.String() in kit/db, so anything
-- else on the setting is a value the kernel did not place, and failing closed on
-- it is the answer.
CREATE OR REPLACE FUNCTION platformkit_current_tenant_id() RETURNS uuid
LANGUAGE sql STABLE PARALLEL SAFE AS $$
	SELECT CASE
		WHEN current_setting('platformkit.tenant_id', true) ~
			'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$'
		THEN current_setting('platformkit.tenant_id', true)::uuid
	END
$$;

-- True inside a db.RunSystem transaction, which is allowed to cross tenants.
CREATE OR REPLACE FUNCTION platformkit_is_system() RETURNS boolean
LANGUAGE sql STABLE PARALLEL SAFE AS $$
	SELECT coalesce(current_setting('platformkit.system_access', true), '') = 'true'
$$;

-- The predicate every tenant-owned policy uses, for both USING and WITH CHECK.
-- Outside any transaction of ours both helpers yield NULL or false, so the
-- policy denies rather than leaks.
CREATE OR REPLACE FUNCTION platformkit_tenant_match(row_tenant_id uuid) RETURNS boolean
LANGUAGE sql STABLE PARALLEL SAFE AS $$
	SELECT platformkit_is_system() OR row_tenant_id = platformkit_current_tenant_id()
$$;
