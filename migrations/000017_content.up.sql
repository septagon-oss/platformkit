-- Content: the pages and posts a tenant's public site is made of.
--
-- The entity is modules/content/contracts/content.go and the columns below are
-- its fields. There is no rendered-HTML column: the body is Markdown and the
-- public route renders it on read, so a change to what the renderer allows
-- applies to everything ever written rather than to whatever is saved next.

CREATE TABLE contents (
	id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id    uuid NOT NULL,
	created_at   timestamptz NOT NULL DEFAULT now(),
	updated_at   timestamptz NOT NULL DEFAULT now(),
	deleted_at   timestamptz,

	-- The name this content is reached by, normalised on every write.
	slug         varchar(200) NOT NULL,
	title        varchar(200) NOT NULL,
	-- Markdown, as it was written.
	body         text NOT NULL DEFAULT '',
	-- page or post.
	kind         varchar(10) NOT NULL DEFAULT 'post',
	-- draft, published or archived.
	status       varchar(10) NOT NULL DEFAULT 'draft',
	-- When it was published, and NULL whenever it is not published: the two are
	-- one fact, and the CHECK below is the half no Go code has to remember.
	published_at timestamptz,
	-- Who created it. It is a user id with no foreign key to users, which is
	-- what "cross-module dependencies are Go interfaces" costs at the database.
	author_id    uuid,

	CONSTRAINT contents_published CHECK ((status = 'published') = (published_at IS NOT NULL))
);

-- Unique per tenant, and partial so deleted content releases its slug.
CREATE UNIQUE INDEX contents_tenant_slug ON contents (tenant_id, slug)
	WHERE deleted_at IS NULL;
-- The default list page.
CREATE INDEX contents_tenant_created ON contents (tenant_id, created_at DESC, id)
	WHERE deleted_at IS NULL;
-- The public read, which is the only query with any volume behind it: one
-- tenant's published rows by name. Partial, so the index is the size of the
-- site rather than of everything anybody has ever drafted.
CREATE INDEX contents_tenant_published ON contents (tenant_id, slug)
	WHERE status = 'published' AND deleted_at IS NULL;

-- The shape every tenant-owned table has; migrations/000001 explains it.
ALTER TABLE contents ENABLE ROW LEVEL SECURITY;
ALTER TABLE contents FORCE ROW LEVEL SECURITY;

CREATE POLICY contents_tenant ON contents
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
