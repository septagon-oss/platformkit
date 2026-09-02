-- Files: one row per uploaded blob.
--
-- The entity is modules/file/contracts/file.go. The row is here and the bytes
-- are wherever the module's Storage puts them, which is the split everything
-- difficult about that module comes from: a blob write cannot be rolled back,
-- so an upload writes the bytes first and a delete removes them afterwards,
-- through file.deleted, once this row's removal has committed.

CREATE TABLE files (
	id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id    uuid NOT NULL,
	created_at   timestamptz NOT NULL DEFAULT now(),
	updated_at   timestamptz NOT NULL DEFAULT now(),
	deleted_at   timestamptz,

	-- What the browser called it, kept for a person to read.
	name         varchar(255) NOT NULL,
	-- What the upload declared. It is not sniffed, and the download answers
	-- with X-Content-Type-Options: nosniff so a browser does not guess either.
	content_type varchar(120) NOT NULL DEFAULT 'application/octet-stream',
	-- What actually arrived, counted and hashed in the one pass that wrote it.
	size         bigint NOT NULL,
	sha256       char(64) NOT NULL,
	-- Where the bytes are: a UUID and nothing else, which is the whole
	-- path-traversal argument — there is no caller-supplied component in a key.
	storage_key  varchar(64) NOT NULL,
	-- private or public. A public file is served at /api/v1/file/public/{id} to
	-- anybody, which is what an image in a published page has to be.
	visibility   varchar(10) NOT NULL DEFAULT 'private',
	-- Who uploaded it, a user id with no foreign key to users.
	uploader_id  uuid,

	CONSTRAINT files_visibility CHECK (visibility IN ('private', 'public')),
	CONSTRAINT files_size CHECK (size >= 0)
);

-- A key is minted per upload, so two rows sharing one is a bug this refuses
-- rather than two records of one blob. It is not partial: a delete removes the
-- row outright, because a soft-deleted row is one that still points at bytes
-- that are about to go.
CREATE UNIQUE INDEX files_storage_key ON files (storage_key);
-- The default list page.
CREATE INDEX files_tenant_created ON files (tenant_id, created_at DESC, id)
	WHERE deleted_at IS NULL;

ALTER TABLE files ENABLE ROW LEVEL SECURITY;
ALTER TABLE files FORCE ROW LEVEL SECURITY;

CREATE POLICY files_tenant ON files
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
