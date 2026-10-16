-- Which languages each tenant is served in.
--
-- The set of languages a request could be answered in used to be a property of the
-- catalog alone: composed once, the same for every tenant of the installation, with
-- a browser's Accept-Language as the only per-request choice. That is decision 0012
-- refused in the other direction — the generic part deciding what the specific part
-- shows — and it is how a deployment that ships one language ends up answering a
-- site it replaced that served two: the catalog says one, nobody asks the tenant.
--
-- Two facts move to the tenant: `default_locale`, the language a request that
-- brought nothing this tenant serves is answered in, and `tenant_locales`, the set
-- Accept-Language is intersected with. `'en'` as the column's default is not a
-- policy about languages; it is the source language of the copy in this repository
-- (kit/entity/display, kit/httpx and ui/* are written in it), so a tenant created
-- before anybody chose a language is answered exactly as it was yesterday.
--
-- The languages themselves are a child table rather than an array column because
-- that is the shape `tenant_hosts` beside it already has: one row per fact, the
-- control plane's own policy over it, and a list the loader can read with the
-- tenant without a second meaning living inside a column.
ALTER TABLE tenants
	ADD COLUMN default_locale text NOT NULL DEFAULT 'en';

CREATE TABLE tenant_locales (
	tenant_id uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
	-- One BCP 47 tag, canonicalised by modules/tenant before it is written, so the
	-- string compared against a negotiated language is the string x/text spells it
	-- as. Lower-case, because this repository's other key-like columns are too.
	locale    text NOT NULL,
	PRIMARY KEY (tenant_id, locale)
);

CREATE INDEX tenant_locales_tenant ON tenant_locales (tenant_id);

ALTER TABLE tenant_locales ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_locales FORCE ROW LEVEL SECURITY;

-- Read your own languages, write none: WITH CHECK is system-only, as it is on
-- tenants and tenant_hosts, so which languages a tenant is served in is the
-- operator's declaration about a customer and not the customer's own setting. What
-- a tenant may decide for itself is its copy, which needs a tenant-scoped table and
-- its own policy — a different thing, briefed separately, not this table with a
-- second meaning.
CREATE POLICY tenant_locales_scope ON tenant_locales
	USING (platformkit_is_system() OR tenant_id = platformkit_current_tenant_id())
	WITH CHECK (platformkit_is_system());

-- No row is written here, and the read is the reason. `localesOf`
-- (modules/tenant/internal/service.go) takes the default out of the set on the way
-- read — `WHERE tenant_id = ? AND locale <> ?` — so the language a tenant is served
-- in by default reaches a request from the column above and never from this table.
-- A tenant whose row predates this file and a tenant created after it answer the
-- same browser in the same language, whether or not a row sits beside the column:
-- the row a create or a `SetLocale` writes records a declaration somebody made about
-- a tenant, which is what a migration makes none of. SQL does not know which
-- catalogues the composition holds, so it could not backfill a set even if the set
-- were read back; it could only ever write the one value the column already holds.
--
-- A `FROM tenants` write here would also be a write that writes nothing at the role
-- this file's own policy describes. The runner sets `platformkit.system_access` on a
-- system transaction and on a `phase=data` drain, and on a schema file's transaction
-- not at all, so `WITH CHECK (platformkit_is_system())` answers such a write as it
-- answers any other, and the policy 000001 puts over `tenants` — "outside any
-- transaction of ours both helpers yield NULL or false, so the policy denies rather
-- than leaks" — empties the source. At a migrate role that owns these tables and is
-- no superuser the statement would be accepted, the version recorded, and no tenant
-- given a language, which is the same outcome as writing nothing, told as a success.
--
-- data: exempt reason: this revision writes no rows, so no `phase=data` half stands
-- beside it; migrations/README.md says what that line means and where a file that
-- does write rows has to take them.

COMMENT ON TABLE tenant_locales IS 'platformkit:tenant-scoping-exempt: control plane, read by the loader under system access';
