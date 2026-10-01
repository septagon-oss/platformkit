-- Retention and erasure: a class on the file, a hold that stops the clock, and
-- a proof that outlives the row.
--
-- One file, transactional, no down file (house rule): ADD COLUMN with a default
-- is a metadata-only change on Postgres 11+, so the table is not rewritten and a
-- deployment with a hundred million files is not locked for the length of one.
--
-- Nothing here is generated from a rest.Spec — modules/file declares none, and a
-- hold is a decision with a reason and an erasure is a receipt. Neither gets a
-- screen.

-- The retention class, as the product that uploaded the file calls it. This
-- module never branches on a value of it and names none: it matches the token
-- against config.Files.Retention and refuses to delete anything it does not
-- find there. The CHECK is on the shape of a token, which is all a token is.
ALTER TABLE files ADD COLUMN kind varchar(32) NOT NULL DEFAULT '';
ALTER TABLE files DROP CONSTRAINT IF EXISTS files_kind;
ALTER TABLE files ADD CONSTRAINT files_kind CHECK (kind = '' OR kind ~ '^[a-z][a-z0-9_]{0,31}$');

-- No index here for the sweep. It reads one tenant's rows, and
-- files_tenant_created (000019) already leads with tenant_id and orders by
-- created_at, which is the shape of its query; kind is a filter over a tenant's
-- own rows, and a partial index on a column the sweep reads once a day is a
-- write cost on every upload for a read that is not on anybody's path. An index
-- on files would also need its own autocommit file with CONCURRENTLY, which is
-- the runner's rule and the reason a second file exists at all.

-- Why a file must outlive its class.
--
-- A row and not a boolean on files, because the two questions a person asks are
-- "is it held" and "who said so and until when", and a boolean answers the first
-- by forgetting the second. UNIQUE on the file, so two people placing a hold is
-- one hold and not two answers to "until when".
--
-- No foreign key to files: a hold is meaningless once the row is gone, so the
-- command that removes the file removes the hold and says who in the trail. A
-- cascade would have removed it silently, which is the difference between a
-- record and a guess.
CREATE TABLE file_holds (
	id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id  uuid NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	deleted_at timestamptz,

	file_id    uuid NOT NULL,
	-- NULL is held until a person releases it, which is what a legal hold is.
	until      timestamptz,
	reason     text NOT NULL,
	placed_by  uuid,

	CONSTRAINT file_holds_file   UNIQUE (tenant_id, file_id),
	CONSTRAINT file_holds_reason CHECK (reason <> ''),
	CONSTRAINT file_holds_until  CHECK (until IS NULL OR until > created_at)
);

CREATE INDEX file_holds_tenant_file ON file_holds (tenant_id, file_id)
	WHERE deleted_at IS NULL;

-- The proof, and it has to outlive the row it proofs.
--
-- storage_key, sha256 and size are copied out of the row before it is deleted,
-- for the reason Deleted carries the storage key today: by the time anybody reads
-- this table there is nothing left to join to.
--
-- verified_at is the whole of the promise and it is NULL until the store has
-- answered that it holds nothing at that name — no object, no version, no delete
-- marker, no abandoned part. versions_seen is what the listing counted, so a
-- bucket with versioning switched on cannot pass this table quietly: the number
-- is above zero and the erasure is reported as unprovable instead of certified.
CREATE TABLE file_erasures (
	id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id     uuid NOT NULL,
	created_at    timestamptz NOT NULL DEFAULT now(),
	updated_at    timestamptz NOT NULL DEFAULT now(),
	deleted_at    timestamptz,

	file_id       uuid NOT NULL,
	storage_key   varchar(64) NOT NULL,
	sha256        char(64)    NOT NULL,
	size          bigint      NOT NULL,
	cause         varchar(16) NOT NULL,
	-- Set for a subject-data erasure and NULL otherwise, so "what did you remove
	-- for this person?" is a WHERE and not a guess. No foreign key: the subject
	-- may itself be deleted from the users table one day, and the proof outlives
	-- that too.
	subject_id    uuid,
	actor         uuid,

	removed_at    timestamptz NOT NULL,
	verified_at   timestamptz,
	versions_seen int NOT NULL DEFAULT 0,

	CONSTRAINT file_erasures_cause CHECK (cause IN ('expired', 'subject', 'caller')),
	-- A redelivered erasure writes one row and not two certificates for one
	-- removal, which is what makes the worker safe to retry.
	CONSTRAINT file_erasures_file  UNIQUE (tenant_id, file_id, cause),
	CONSTRAINT file_erasures_size  CHECK (size >= 0),
	CONSTRAINT file_erasures_seen  CHECK (versions_seen >= 0)
);

CREATE INDEX file_erasures_tenant_subject ON file_erasures (tenant_id, subject_id, removed_at DESC)
	WHERE subject_id IS NOT NULL;
CREATE INDEX file_erasures_tenant_removed ON file_erasures (tenant_id, removed_at DESC);

-- Both tables, the same policy files has had since 000019: a tenant's own rows,
-- or nothing, whatever the SQL says. FORCE so the table's owner is bound by it
-- too, and platformkit_tenant_match is the one function that decides, so a
-- module cannot write a wider one.
ALTER TABLE file_holds ENABLE ROW LEVEL SECURITY;
ALTER TABLE file_holds FORCE ROW LEVEL SECURITY;
CREATE POLICY file_holds_tenant ON file_holds
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));

ALTER TABLE file_erasures ENABLE ROW LEVEL SECURITY;
ALTER TABLE file_erasures FORCE ROW LEVEL SECURITY;
CREATE POLICY file_erasures_tenant ON file_erasures
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
