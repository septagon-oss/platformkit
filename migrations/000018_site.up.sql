-- Site: one tenant's public site settings, and there is one row or none.
--
-- The entity is modules/site/contracts/site.go. There is nothing here about
-- rendering: a theme reads the title, the navigation and the colour and decides
-- what to do with them, and this table is what it reads.

CREATE TABLE site_settings (
	id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id     uuid NOT NULL,
	created_at    timestamptz NOT NULL DEFAULT now(),
	updated_at    timestamptz NOT NULL DEFAULT now(),
	deleted_at    timestamptz,

	title         varchar(120) NOT NULL DEFAULT '',
	tagline       varchar(200) NOT NULL DEFAULT '',
	-- The slug of the content served at the site's root. It is a slug and not
	-- an id because a page can be rewritten and replaced and still be the home
	-- page, and there is no foreign key to contents: this module never names
	-- the one that serves it.
	home_slug     varchar(200) NOT NULL DEFAULT '',
	-- light, dark, or system to follow the visitor's own preference.
	theme         varchar(10) NOT NULL DEFAULT 'system',
	-- #rrggbb, lower case.
	primary_color char(7) NOT NULL DEFAULT '#2563eb',
	-- A file id with no foreign key to files, for the same reason as home_slug:
	-- a logo that has been deleted is a site that renders without one.
	logo_file_id  uuid,
	-- The navigation, in the order it is shown: [{"label":…,"path":…}].
	nav           jsonb NOT NULL DEFAULT '[]'
);

-- One site per tenant, which is why the routes are a read and a PUT rather than
-- a collection. Partial, so a deleted row would let a tenant start again.
CREATE UNIQUE INDEX site_settings_tenant ON site_settings (tenant_id)
	WHERE deleted_at IS NULL;

ALTER TABLE site_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE site_settings FORCE ROW LEVEL SECURITY;

CREATE POLICY site_settings_tenant ON site_settings
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
