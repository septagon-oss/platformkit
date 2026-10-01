-- Change control: proposals, one per change somebody wants somebody else to look
-- at. The entity is modules/change/contracts/change.go and the columns are its
-- fields; the module owns the SQL and the state machine beside it.
--
-- The three facts this table exists to keep are the diff, the digest of the exact
-- bytes a verdict was made about, and who each actor was. The first two are
-- columns rather than something recomputed on read because the answer has to
-- survive the subject being rewritten a dozen times, and the third is a column
-- because no log file survives long enough to answer it.

CREATE TABLE change_proposals (
	id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id      uuid NOT NULL,
	created_at     timestamptz NOT NULL DEFAULT now(),
	updated_at     timestamptz NOT NULL DEFAULT now(),
	deleted_at     timestamptz,

	-- The row this change would alter, named rather than referenced: a foreign key
	-- into another module's table is a coupling the module boundary does not allow,
	-- for the reason modules/task gives for source_ref. subject_id is the nil uuid
	-- for an entity with one row per tenant, and NOT NULL so the unique index below
	-- cannot be defeated by NULL's idea of equality.
	subject_module   varchar(40)  NOT NULL,
	subject_entity   varchar(40)  NOT NULL,
	subject_id       uuid         NOT NULL,
	base_revision    bigint       NOT NULL,
	-- The change (RFC 7386) and the digest of its canonical bytes. Both, because
	-- apply runs the first and a verdict is about the second.
	diff             jsonb        NOT NULL,
	diff_digest      text         NOT NULL,
	summary          varchar(200) NOT NULL,
	-- Who put it forward, from their own credentials, and who decided. The reviewer
	-- is a separate column from the verdict so a decision can be quoted without
	-- parsing one.
	proposer         uuid         NOT NULL,
	reviewer         uuid,
	verdict          varchar(10)  NOT NULL DEFAULT ''
		CONSTRAINT change_proposals_verdict CHECK (verdict IN ('', 'approved', 'declined')),
	comment          text         NOT NULL DEFAULT '',
	reviewed_at      timestamptz,
	-- The subject revision this proposal produced: zero means it has not been
	-- applied, and applied is terminal, so updated_at is the moment it was.
	applied_revision bigint       NOT NULL DEFAULT 0
		CONSTRAINT change_proposals_applied_revision CHECK (applied_revision >= 0),
	state            varchar(20)  NOT NULL DEFAULT 'proposed'
		CONSTRAINT change_proposals_state CHECK (state IN ('proposed', 'approved', 'declined', 'withdrawn', 'applied')),
	-- This row's own revision, which every command rechecks inside its transaction.
	revision         bigint       NOT NULL DEFAULT 1
		CONSTRAINT change_proposals_revision CHECK (revision >= 1)
);

-- One open proposal per subject and per diff: the same opinion submitted twice is
-- one row, and the index is what makes that true under two simultaneous submits
-- rather than only under one. Partial, so the history of decided proposals is not
-- held open by a uniqueness rule about live ones.
CREATE UNIQUE INDEX change_proposals_open ON change_proposals
	(tenant_id, subject_module, subject_entity, subject_id, diff_digest)
	WHERE state IN ('proposed', 'approved');

-- The two questions a screen asks: what is waiting for a decision, and what is
-- pending on this row. Both prefixed with tenant_id, because every one of them
-- runs under a policy that has already narrowed the table to one tenant.
CREATE INDEX change_proposals_tenant_state ON change_proposals (tenant_id, state, created_at DESC, id);
CREATE INDEX change_proposals_tenant_subject ON change_proposals (tenant_id, subject_module, subject_entity, subject_id);

ALTER TABLE change_proposals ENABLE ROW LEVEL SECURITY;
ALTER TABLE change_proposals FORCE ROW LEVEL SECURITY;

CREATE POLICY change_proposals_tenant ON change_proposals
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
