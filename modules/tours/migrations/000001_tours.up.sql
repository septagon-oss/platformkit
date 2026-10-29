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

CREATE INDEX tours_runs_person ON tours_runs (tenant_id, app, person_id, created_at DESC);
CREATE INDEX tours_runs_edition ON tours_runs (tenant_id, app, person_id, tour_id, edition, source);

ALTER TABLE tours_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE tours_runs FORCE ROW LEVEL SECURITY;
CREATE POLICY tours_runs_tenant ON tours_runs
    USING (platformkit_tenant_match(tenant_id))
    WITH CHECK (platformkit_tenant_match(tenant_id));
