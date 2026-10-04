-- A tenant administrator sets their own tenant's languages.
--
-- Which languages a customer is served in was the operator's declaration about
-- them. That was right about the deployment's catalogues and wrong about the
-- customer: an administrator who wants their people answered in Portuguese had to
-- open a ticket with whoever runs the installation, and the one surface that could
-- answer the question — this tenant's own settings — refused the request it is
-- about. `SetLocale` stays what it is, a control-plane command; this opens a
-- tenant-self door beside it onto the same fact, the same event and the same
-- normalisation, through `SetOwnLocale`.
--
-- The old `tenants_scope` policy permitted more than its comment claimed. It put
-- `USING (system OR id = current_tenant)` and `WITH CHECK (system)` on a policy
-- with no FOR clause, so it covered every command — and PostgreSQL applies
-- WITH CHECK to inserted and updated rows only, never to a DELETE
-- (https://www.postgresql.org/docs/16/sql-createpolicy.html). The `USING` clause
-- was therefore a self-DELETE grant on the row that says whether a customer exists
-- and what it is called. This file splits the policy by command so each clause
-- means what it says: read your own row, change your own languages, and delete
-- nothing.
--
-- `locale_revision` counts the declarations this tenant has made. Unlike the user
-- preference this task also moved, the fact has two writers — an administrator
-- through this door and an operator through the control plane — and a stale
-- operator page overwriting an administrator's choice is exactly the lost update a
-- revision is for. Both commands compare it and both move it.
ALTER TABLE tenants
	ADD COLUMN locale_revision bigint NOT NULL DEFAULT 0 CHECK (locale_revision >= 0);

DROP POLICY tenants_scope ON tenants;

-- Read your own row, as before; the loader resolves a host through it on every
-- request, which is a system read.
CREATE POLICY tenants_read ON tenants FOR SELECT
	USING (platformkit_is_system() OR id = platformkit_current_tenant_id());

-- Only the control plane creates a tenant. An operator permission is refused
-- outside the operator's own tenant (contracts.CheckedPermissions, docs/adr/0008),
-- so no customer can reach this branch through a wildcard of its own.
CREATE POLICY tenants_create ON tenants FOR INSERT
	WITH CHECK (platformkit_is_system());

-- Update your own row: the row-identity rule is here, and the question of *which
-- columns* is not a row-identity question, so it belongs to the trigger below —
-- PostgreSQL has no column-level policy. A self UPDATE whose WITH CHECK were
-- system-only would make this file's own subject impossible, and a self UPDATE
-- without the trigger would let a tenant rename or suspend itself.
CREATE POLICY tenants_update ON tenants FOR UPDATE
	USING (platformkit_is_system() OR id = platformkit_current_tenant_id())
	WITH CHECK (platformkit_is_system() OR id = platformkit_current_tenant_id());

-- The half the old policy permitted and never claimed: deleting a tenant is the
-- control plane's, and no tenant transaction may do it to itself.
CREATE POLICY tenants_delete ON tenants FOR DELETE
	USING (platformkit_is_system());

-- The languages themselves. The set is one row per tag, so a self transaction
-- needs all three write commands to replace it, and every one of them is scoped to
-- rows that name this tenant: the `tenant_id` is compared to the same helper the
-- read is, and another tenant's rows are invisible rather than merely refused.
DROP POLICY tenant_locales_scope ON tenant_locales;

CREATE POLICY tenant_locales_read ON tenant_locales FOR SELECT
	USING (platformkit_is_system() OR tenant_id = platformkit_current_tenant_id());

CREATE POLICY tenant_locales_create ON tenant_locales FOR INSERT
	WITH CHECK (platformkit_is_system() OR tenant_id = platformkit_current_tenant_id());

CREATE POLICY tenant_locales_update ON tenant_locales FOR UPDATE
	USING (platformkit_is_system() OR tenant_id = platformkit_current_tenant_id())
	WITH CHECK (platformkit_is_system() OR tenant_id = platformkit_current_tenant_id());

CREATE POLICY tenant_locales_delete ON tenant_locales FOR DELETE
	USING (platformkit_is_system() OR tenant_id = platformkit_current_tenant_id());

-- tenants_language_only: the column rule the policy above cannot state.
--
-- A tenant transaction may change its own languages and nothing else on its own
-- row: `slug`, `name`, `status` and `operator` are the control plane's facts about
-- the customer, `created_at` is a fact about the past, and `deleted_at` is the fact
-- of whether the customer exists at all — which the split policy above refuses a
-- tenant a DELETE of, and a soft delete through an UPDATE would have been the same
-- refusal walked around by the one command the tenant is granted. `default_locale`,
-- `locale_revision` and `updated_at` are what the self-service command writes, and
-- a system transaction changes whatever it likes — the operator's `SetLocale`,
-- `AddHost`, `Suspend` and `Create` keep every authority they had, because
-- `platformkit_is_system()` is what the kernel sets on a transaction of ours and
-- nothing else.
--
-- The refusal is an exception and not a quiet no-op: the write is lost, the
-- transaction aborts, and the caller is told which rule it broke. The application
-- role is subject to this even as the table's own grantee, because the trigger
-- asks who the transaction *is* rather than what the role *owns*, and the review
-- that asked for it insisted it be proved at the application role rather than
-- inferred from the SQL. It does, column by column, in
-- modules/tenant/internal/own_row_column_rule_test.go.
CREATE OR REPLACE FUNCTION tenants_language_only() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
	IF platformkit_is_system() THEN
		RETURN NEW;
	END IF;
	IF NEW.slug IS DISTINCT FROM OLD.slug
		OR NEW.name IS DISTINCT FROM OLD.name
		OR NEW.status IS DISTINCT FROM OLD.status
		OR NEW.operator IS DISTINCT FROM OLD.operator
		OR NEW.created_at IS DISTINCT FROM OLD.created_at
		OR NEW.deleted_at IS DISTINCT FROM OLD.deleted_at THEN
		RAISE EXCEPTION 'a tenant transaction may change only its own languages'
			USING ERRCODE = '42501';
	END IF;
	RETURN NEW;
END;
$$;

CREATE TRIGGER tenants_language_only
	BEFORE UPDATE ON tenants
	FOR EACH ROW EXECUTE FUNCTION tenants_language_only();

-- No row is written: a column with a default and four policies that widen what may
-- be read and written.
--
-- data: exempt reason: this revision writes no rows, so no `phase=data` half stands
-- beside it; migrations/README.md says what that line means and where a file that
-- does write rows has to take them.

COMMENT ON TABLE tenants IS 'platformkit:tenant-scoping-exempt: control plane, read by the loader under system access, self-updatable in its language columns only';
