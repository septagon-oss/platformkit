CREATE TABLE tours_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    app text NOT NULL,
    person_id uuid NOT NULL,
    tour_id text NOT NULL,
    edition integer NOT NULL CHECK (edition > 0),
    content_revision text NOT NULL,
    contract_hash text NOT NULL,
    step_ids jsonb NOT NULL,
    source text NOT NULL CHECK (source IN ('automatic', 'manual')),
    status text NOT NULL CHECK (status IN ('reserved', 'active', 'paused', 'later', 'completed', 'dismissed', 'exited')),
    current_step text NOT NULL,
    done jsonb NOT NULL DEFAULT '[]'::jsonb,
    skipped jsonb NOT NULL DEFAULT '[]'::jsonb,
    revision bigint NOT NULL CHECK (revision > 0),
    later_until timestamptz,
    started_at timestamptz NOT NULL,
    viewed_at timestamptz,
    completed_at timestamptz,
    dismissed_at timestamptz,
    UNIQUE (tenant_id, id)
);

CREATE TABLE tours_person_state (
    tenant_id uuid NOT NULL,
    app text NOT NULL,
    person_id uuid NOT NULL,
    tour_id text NOT NULL,
    edition integer NOT NULL CHECK (edition > 0),
    completed_at timestamptz,
    dismissed_at timestamptz,
    retired_at timestamptz,
    automatic_run_id uuid,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, app, person_id, tour_id, edition)
);

CREATE TABLE tours_person_frequency (
    tenant_id uuid NOT NULL,
    app text NOT NULL,
    person_id uuid NOT NULL,
    last_automatic_viewed_at timestamptz,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, app, person_id)
);

CREATE TABLE tours_reservations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    app text NOT NULL,
    person_id uuid NOT NULL,
    tour_id text NOT NULL,
    edition integer NOT NULL CHECK (edition > 0),
    run_id uuid NOT NULL,
    reserved_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    released_at timestamptz,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    CHECK (expires_at > reserved_at),
    UNIQUE (tenant_id, id)
);

CREATE UNIQUE INDEX tours_reservations_one_live_person
    ON tours_reservations (tenant_id, app, person_id)
    WHERE consumed_at IS NULL AND released_at IS NULL;

CREATE INDEX tours_runs_person ON tours_runs (tenant_id, app, person_id, created_at DESC);
CREATE INDEX tours_runs_edition ON tours_runs (tenant_id, app, person_id, tour_id, edition, source);

ALTER TABLE tours_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE tours_runs FORCE ROW LEVEL SECURITY;
CREATE POLICY tours_runs_tenant ON tours_runs
    USING (platformkit_tenant_match(tenant_id))
    WITH CHECK (platformkit_tenant_match(tenant_id));

ALTER TABLE tours_person_state ENABLE ROW LEVEL SECURITY;
ALTER TABLE tours_person_state FORCE ROW LEVEL SECURITY;
CREATE POLICY tours_person_state_tenant ON tours_person_state
    USING (platformkit_tenant_match(tenant_id))
    WITH CHECK (platformkit_tenant_match(tenant_id));

ALTER TABLE tours_person_frequency ENABLE ROW LEVEL SECURITY;
ALTER TABLE tours_person_frequency FORCE ROW LEVEL SECURITY;
CREATE POLICY tours_person_frequency_tenant ON tours_person_frequency
    USING (platformkit_tenant_match(tenant_id))
    WITH CHECK (platformkit_tenant_match(tenant_id));

ALTER TABLE tours_reservations ENABLE ROW LEVEL SECURITY;
ALTER TABLE tours_reservations FORCE ROW LEVEL SECURITY;
CREATE POLICY tours_reservations_tenant ON tours_reservations
    USING (platformkit_tenant_match(tenant_id))
    WITH CHECK (platformkit_tenant_match(tenant_id));
