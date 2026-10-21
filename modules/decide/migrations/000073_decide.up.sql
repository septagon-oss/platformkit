-- Questions serialize roster, admission, lifecycle and ballot decisions.
CREATE TABLE decide_questions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    deleted_at timestamptz,
    title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 10000),
    closes_at timestamptz,
    allow_link_join boolean NOT NULL DEFAULT false,
    status text NOT NULL CHECK (status IN ('open', 'closed')),
    creator_id uuid NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    closed_at timestamptz,
    invite_generation bigint NOT NULL DEFAULT 0 CHECK (invite_generation >= 0),
    invite_expires_at timestamptz,
    UNIQUE (tenant_id, id),
    CHECK (invite_expires_at IS NULL OR invite_generation > 0),
    CHECK ((status = 'closed') = (closed_at IS NOT NULL))
);
CREATE INDEX decide_questions_list ON decide_questions (tenant_id, created_at DESC, id);
CREATE INDEX decide_questions_due ON decide_questions (tenant_id, closes_at, id)
    WHERE status = 'open' AND closes_at IS NOT NULL;

CREATE TABLE decide_options (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    deleted_at timestamptz,
    question_id uuid NOT NULL,
    title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    image_id uuid,
    note text NOT NULL DEFAULT '' CHECK (char_length(note) <= 2000),
    link text NOT NULL DEFAULT '' CHECK (octet_length(link) <= 2048),
    position bigint NOT NULL CHECK (position >= 0),
    revision bigint NOT NULL CHECK (revision > 0),
    removed_at timestamptz,
    first_matched_at timestamptz,
    UNIQUE (tenant_id, question_id, id),
    FOREIGN KEY (tenant_id, question_id) REFERENCES decide_questions (tenant_id, id)
);
CREATE INDEX decide_options_order ON decide_options (tenant_id, question_id, position, id);

CREATE TABLE decide_participants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    deleted_at timestamptz,
    question_id uuid NOT NULL,
    person_id uuid NOT NULL,
    joined_at timestamptz NOT NULL,
    revision bigint NOT NULL CHECK (revision = 1),
    UNIQUE (tenant_id, question_id, person_id),
    FOREIGN KEY (tenant_id, question_id) REFERENCES decide_questions (tenant_id, id)
);
CREATE INDEX decide_participants_person ON decide_participants (tenant_id, person_id, question_id);

CREATE TABLE decide_votes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    deleted_at timestamptz,
    question_id uuid NOT NULL,
    option_id uuid NOT NULL,
    person_id uuid NOT NULL,
    value text CHECK (value IN ('yes', 'strong', 'no', 'skip')),
    at timestamptz NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    UNIQUE (tenant_id, question_id, option_id, person_id),
    FOREIGN KEY (tenant_id, question_id, option_id) REFERENCES decide_options (tenant_id, question_id, id),
    FOREIGN KEY (tenant_id, question_id, person_id) REFERENCES decide_participants (tenant_id, question_id, person_id)
);

ALTER TABLE decide_questions ENABLE ROW LEVEL SECURITY;
ALTER TABLE decide_questions FORCE ROW LEVEL SECURITY;
CREATE POLICY decide_questions_tenant ON decide_questions
    USING (platformkit_tenant_match(tenant_id)) WITH CHECK (platformkit_tenant_match(tenant_id));
ALTER TABLE decide_options ENABLE ROW LEVEL SECURITY;
ALTER TABLE decide_options FORCE ROW LEVEL SECURITY;
CREATE POLICY decide_options_tenant ON decide_options
    USING (platformkit_tenant_match(tenant_id)) WITH CHECK (platformkit_tenant_match(tenant_id));
ALTER TABLE decide_participants ENABLE ROW LEVEL SECURITY;
ALTER TABLE decide_participants FORCE ROW LEVEL SECURITY;
CREATE POLICY decide_participants_tenant ON decide_participants
    USING (platformkit_tenant_match(tenant_id)) WITH CHECK (platformkit_tenant_match(tenant_id));
ALTER TABLE decide_votes ENABLE ROW LEVEL SECURITY;
ALTER TABLE decide_votes FORCE ROW LEVEL SECURITY;
CREATE POLICY decide_votes_tenant ON decide_votes
    USING (platformkit_tenant_match(tenant_id)) WITH CHECK (platformkit_tenant_match(tenant_id));
