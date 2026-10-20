-- A person's two doors, and the rows that say a door was asked for.
--
-- modules/subject owns no person's rows: the export reads eight other modules and the
-- erasure rewrites them. What this module owns is the *request* — the record that a
-- person asked for their copy, when, who was told, and which bundle answers them — and
-- the one-time link that lets them open it. ADR 0020 §5 and §6 are the decision; these
-- two tables are its bookkeeping.
--
-- The request row exists because the work is not a request's business: building a bundle
-- is walking nine owners, and the answer to "where is my export" has to survive the
-- browser being closed. It is `requested`, `running`, `ready`, `erased` or `failed`, and
-- the state is what the worker walks, so the state and the walk are the same fact.

CREATE TABLE subject_requests (
	id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id      uuid NOT NULL,
	created_at     timestamptz NOT NULL DEFAULT now(),
	updated_at     timestamptz NOT NULL DEFAULT now(),

	-- The person the request is about, and the one who asked. Both are columns rather
	-- than one, because the two are not always the same actor and the difference is the
	-- audit answer: a person asking for their own copy and an officer producing one for
	-- them are different facts, and "who asked" has to stay answerable after the person
	-- they asked about is erased (ADR 0020 §7).
	subject_id     uuid NOT NULL,
	requested_by   uuid NOT NULL,

	kind           varchar(10) NOT NULL
		CONSTRAINT subject_requests_kind CHECK (kind IN ('export', 'erase')),
	state          varchar(12) NOT NULL DEFAULT 'requested'
		CONSTRAINT subject_requests_state CHECK (state IN ('requested', 'running', 'ready', 'erased', 'failed')),

	-- The reason the person gave, and the reason the work stopped. Both free text, both
	-- declared as columns an identifier may have been copied into, so an erasure scrubs
	-- them with the same statement every other owner's text is scrubbed with.
	reason         text NOT NULL DEFAULT '',
	detail         text NOT NULL DEFAULT '',

	-- The bundle this request produced: the file row that holds it, its digest, and when
	-- the link to it dies. Empty and NULL until the walk finishes, and `detail` says why
	-- it never did.
	bundle_file_id uuid         NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000'::uuid,
	bundle_digest  varchar(64)  NOT NULL DEFAULT '',
	link_expires_at timestamptz,

	revision       bigint NOT NULL DEFAULT 1
		CONSTRAINT subject_requests_revision CHECK (revision >= 1)
);

-- Two erasures of one person are one erasure (ADR 0020, Consequences). The export kind is
-- deliberately outside the index: a second export of an unchanged person is a different
-- moment in time and a new bundle, which is what §7 says. Partial, so an export never
-- collides with the erasure of the same person.
CREATE UNIQUE INDEX subject_requests_one_erasure ON subject_requests (tenant_id, subject_id)
	WHERE kind = 'erase';

-- The worker's own question, and the person's: what is waiting, and what did I ask for.
CREATE INDEX subject_requests_tenant_open ON subject_requests (tenant_id, kind, state, created_at, id);
CREATE INDEX subject_requests_tenant_subject ON subject_requests (tenant_id, subject_id, created_at DESC);

ALTER TABLE subject_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE subject_requests FORCE ROW LEVEL SECURITY;

CREATE POLICY subject_requests_tenant ON subject_requests
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));

-- One row per minted link, the token stored hashed — the rule sessions and password
-- tokens follow (`sessions.token_hash`, `password_tokens`), so a database read never
-- hands out a working link, and the row is the only place a link's life is recorded.
--
-- The token is minted by this module rather than by `file.Grant` because the file
-- module's own ceiling is `MaxGrantExpiry` (24 hours) and the export link lives seven
-- days: raising that constant would widen the door every upload in the product walks
-- through, for a reason that is one command's (ADR 0020 §6).
CREATE TABLE subject_grants (
	id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id      uuid NOT NULL,
	created_at     timestamptz NOT NULL DEFAULT now(),

	-- No foreign key: a reference into another module's table is a coupling the boundary
	-- does not allow, and the request id is only ever resolved from inside this module's
	-- own join.
	request_id     uuid         NOT NULL,
	token_hash     varchar(64)  NOT NULL,
	expires_at     timestamptz  NOT NULL,
	-- Set by the redeem, in the statement that spends the token, so a link is spent by
	-- the read that shows it and not by a later sweep noticing.
	redeemed_at    timestamptz
);

-- Two links never share a token, and the redeem looks a token up by nothing else.
CREATE UNIQUE INDEX subject_grants_token ON subject_grants (tenant_id, token_hash);
CREATE INDEX subject_grants_request ON subject_grants (tenant_id, request_id);

ALTER TABLE subject_grants ENABLE ROW LEVEL SECURITY;
ALTER TABLE subject_grants FORCE ROW LEVEL SECURITY;

CREATE POLICY subject_grants_tenant ON subject_grants
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
