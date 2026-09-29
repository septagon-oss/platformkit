-- Which languages each tenant is served in.
--
-- The set of languages a request could be answered in used to be a property of the
-- catalog alone: composed once, the same for every tenant of the installation, with
-- a browser's Accept-Language as the only per-request choice. That is decision 0012
-- refused in the other direction — the generic part deciding what the specific part
-- shows — and it is why `collect` could drop `web.locales` as "one language" while
-- the site it replaced served Portuguese and English.
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

-- Every tenant that exists today is served in the one language it was served in
-- yesterday, which is what the column's default says, so the set is backfilled from
-- the column rather than left empty: an empty set reads as "this tenant declared
-- nothing", and a deployment must not change which languages it answers in because
-- somebody ran a migration.
INSERT INTO tenant_locales (tenant_id, locale)
SELECT id, default_locale FROM tenants
ON CONFLICT (tenant_id, locale) DO NOTHING;

COMMENT ON TABLE tenant_locales IS 'platformkit:tenant-scoping-exempt: control plane, read by the loader under system access';
