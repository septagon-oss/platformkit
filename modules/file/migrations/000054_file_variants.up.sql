-- One row per (file, width): the plan an image's variants are, and what the
-- encode made of it.
--
-- A variant is not a file. It has no row in files, so it is never listed as a
-- file, never opened as one, and never counted as a use of its own — it is the
-- same picture at one width, and it dies with its parent, in the transaction
-- that removes the parent, which is why there is no file_id foreign key here
-- pretending to be the rule: the rows are removed by the same commands that
-- remove files rows, and internal/retention.go's erasure path takes their
-- storage keys with the delete event because by the time a worker runs there is
-- no row left to read them from.
--
-- The plan is filed at upload, one pending row per width the frame is smaller
-- than (contracts.VariantWidths), and the encode is a clock job: kit/jobs is
-- where work triggered by the clock lives, the pending rows are the queue, and
-- UNIQUE (tenant_id, file_id, width) below is the idempotency key — a retried
-- upload, a second tick and a redelivery all file nothing twice.
CREATE TABLE file_variants (
	id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id    uuid NOT NULL,
	created_at   timestamptz NOT NULL DEFAULT now(),
	updated_at   timestamptz NOT NULL DEFAULT now(),
	deleted_at   timestamptz,

	file_id      uuid        NOT NULL,
	width        int         NOT NULL,
	height       int         NOT NULL DEFAULT 0,
	state        varchar(8)  NOT NULL DEFAULT 'pending',
	reason       varchar(24) NOT NULL DEFAULT '',
	content_type varchar(120) NOT NULL DEFAULT '',
	size         bigint      NOT NULL DEFAULT 0,
	sha256       char(64)    NOT NULL DEFAULT '',
	storage_key  varchar(64) NOT NULL,

	-- The idempotency key, and the only thing that makes an encode repeatable:
	-- one row per width per file, whatever filed it and however many times.
	CONSTRAINT file_variants_one_per_width UNIQUE (tenant_id, file_id, width),
	-- The closed set the door will serve and the srcset can name. A width here
	-- that contracts.VariantWidthSet does not list would be a plan nothing can
	-- answer, and files_widths has no such CHECK because a frame is any width:
	-- this table's widths are the deployment's addressable set.
	CONSTRAINT file_variants_width CHECK (width IN (480, 960, 1440, 1920)),
	CONSTRAINT file_variants_state CHECK (state IN ('pending', 'ready', 'failed')),
	-- A reason belongs to a failure and to nothing else: a pending row that
	-- explained itself would be a row that had already given up.
	CONSTRAINT file_variants_reason CHECK ((state = 'failed') = (reason <> '')),
	-- ready means the bytes exist and the row can describe them: an object with
	-- no size, no digest and no height is a row that lied about an encode.
	CONSTRAINT file_variants_ready CHECK (
		(state = 'ready') = (size > 0 AND sha256 <> '' AND height > 0 AND content_type <> '')),
	-- A width that is not ready has no bytes, so it charges for none. The quota
	-- is a sum over this column and files.size, and pending rows are what makes
	-- filing a plan free.
	CONSTRAINT file_variants_pending_is_free CHECK (state = 'ready' OR size = 0),
	CONSTRAINT file_variants_size CHECK (size >= 0 AND height >= 0)
);

-- Two reads, each with an index. The partial one is the job's claim: every five
-- minutes it asks "what is still pending for this tenant", and a table of ready
-- rows is what every image older than one tick looks like, so an index over all
-- of them would be a scan of everything finished to find the one row new. The
-- other is the srcset's — one read of one file's widths per rendered image.
CREATE INDEX file_variants_pending ON file_variants (tenant_id, state, created_at, id)
	WHERE state = 'pending';
CREATE INDEX file_variants_file ON file_variants (tenant_id, file_id);

-- The same policy files and file_uses have had, for the same reason: the USING
-- half makes another tenant's variant unfindable and the WITH CHECK half
-- refuses the row, so a variant of another tenant's picture cannot be written
-- here rather than merely not read back.
ALTER TABLE file_variants ENABLE ROW LEVEL SECURITY;
ALTER TABLE file_variants FORCE ROW LEVEL SECURITY;
CREATE POLICY file_variants_tenant ON file_variants
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
