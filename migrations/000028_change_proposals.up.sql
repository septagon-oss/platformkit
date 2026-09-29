-- Change proposals: kit/change's one table, the kernel's generic "a change is proposed, reviewed by another
-- account against an exact revision, then applied".
--
-- A row is one proposal about one subject (subject_kind + subject_id, any entity of any module) at one
-- revision of it (an opaque string the subject's owner chooses: an updated_at, a version, a digest). The
-- lifecycle is pending -> approved | rejected | withdrawn, then approved -> applied; kit/change moves it and
-- publishes an event for every transition, which modules/audit records. The table keeps the proposal's own
-- facts — who proposed, who reviewed, when — so the history is readable from the row as well as the trail.
CREATE TABLE change_proposals (
	id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id    uuid NOT NULL,
	created_at   timestamptz NOT NULL DEFAULT now(),
	updated_at   timestamptz NOT NULL DEFAULT now(),

	subject_kind text NOT NULL CHECK (subject_kind <> ''),
	subject_id   text NOT NULL CHECK (subject_id <> ''),
	revision     text NOT NULL CHECK (revision <> ''),
	-- What the change is, as the subject's owner encodes it. kit/change never reads it.
	diff         jsonb NOT NULL DEFAULT '{}'::jsonb,
	summary      text NOT NULL DEFAULT '',

	state        text NOT NULL DEFAULT 'pending'
		CHECK (state IN ('pending', 'approved', 'rejected', 'withdrawn', 'applied')),
	proposed_by  uuid NOT NULL,
	reviewed_by  uuid,
	reviewed_at  timestamptz,
	applied_at   timestamptz,
	reason       text NOT NULL DEFAULT '',

	-- Nobody reviews their own proposal: the rule is the kernel's and also the table's.
	CONSTRAINT change_proposals_other_reviewer CHECK (reviewed_by IS NULL OR reviewed_by <> proposed_by)
);

CREATE INDEX change_proposals_subject ON change_proposals (tenant_id, subject_kind, subject_id, created_at DESC);

ALTER TABLE change_proposals ENABLE ROW LEVEL SECURITY;
ALTER TABLE change_proposals FORCE ROW LEVEL SECURITY;

CREATE POLICY change_proposals_tenant ON change_proposals
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
