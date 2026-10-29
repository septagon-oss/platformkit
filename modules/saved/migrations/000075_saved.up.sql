-- Saved: one person's retained copy of a source they were allowed to save. The
-- entity is modules/saved/contracts/saved.go; the module owns this SQL as "saved".
--
-- There is no foreign key to any source table on purpose. A retained copy has to
-- outlive the thing it was taken from — that is the whole capability — and a key
-- here would either cascade the copy away with the source or block the deletion
-- the source's own owner is entitled to make.

CREATE TABLE saved_items (
	id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id      uuid NOT NULL,
	created_at     timestamptz NOT NULL DEFAULT now(),
	updated_at     timestamptz NOT NULL DEFAULT now(),
	deleted_at     timestamptz,

	person_id      uuid NOT NULL,
	source_module  varchar(40) NOT NULL,
	source_kind    varchar(40) NOT NULL,
	source_key     varchar(512) NOT NULL,
	revision       bigint NOT NULL CHECK (revision = 1),

	title          varchar(512) NOT NULL,
	locale         varchar(64) NOT NULL,
	source_version varchar(128) NOT NULL,
	-- The capture is kept as the bytes it arrived as, so no JSON number round
	-- trips through a float on the way back out. The name is the SQL keyword
	-- spelled bare: Postgres reads it as this column in every position this
	-- module and kit/personaldata put it in, and kit/personaldata reads only
	-- bare names out of this file, which is what lets the module file it as
	-- text an identifier may sit in and be refused if it then does not scrub it.
	values         text NOT NULL,

	-- A picture is either an immutable file or one composed image with the recipe
	-- that produced it, never both and never half: a copy that displayed a file
	-- must keep displaying that file when the file's own tenant-side record moves.
	picture_file_id            uuid,
	picture_svg                text NOT NULL DEFAULT '',
	picture_template_digest    varchar(64) NOT NULL DEFAULT '',
	picture_value              text NOT NULL DEFAULT '',
	CHECK (picture_file_id IS NULL OR (picture_svg = '' AND picture_template_digest = '' AND picture_value = '')),
	CHECK ((picture_svg = '') = (picture_template_digest = '') AND (picture_svg = '') = (picture_value = '')),

	captured_at    timestamptz NOT NULL
);

-- One copy per person per source: the second save of one source is the same
-- copy, and this index is what makes two first saves of it one row rather than
-- one error.
CREATE UNIQUE INDEX saved_items_person_source ON saved_items
	(tenant_id, person_id, source_module, source_kind, source_key);
-- The two reads the library page makes: everything this person saved, newest
-- first, and one kind of it. Both carry tenant_id first because the policy has
-- already narrowed the table to one tenant, and an index that does not say so
-- makes Postgres read the other tenants' entries in order to discard them.
CREATE INDEX saved_items_person_recent ON saved_items (tenant_id, person_id, captured_at DESC, id DESC);
CREATE INDEX saved_items_person_kind   ON saved_items (tenant_id, person_id, source_kind, captured_at DESC, id DESC);

-- The shape every tenant-owned table has; migrations/000001 explains it. FORCE
-- is what binds the owner, and without it the application role escapes the
-- policy on any table it created itself. Person scope is not a policy: it is a
-- predicate in every statement this module writes, so that one query path
-- carries it rather than a second mechanism that could be forgotten.
ALTER TABLE saved_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE saved_items FORCE ROW LEVEL SECURITY;

CREATE POLICY saved_items_tenant ON saved_items
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
