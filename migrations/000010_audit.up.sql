-- The audit trail: one row per event, per tenant, written by a subscriber.
--
-- modules/audit subscribes to every event every other module declares, so this
-- table is the outbox's history rather than a second account of it. That is why
-- there is no entity_type, no before/after and no action taxonomy: what
-- happened is the event name and the payload the module already publishes, and
-- a schema that tried to normalise those would be a schema every new module had
-- to be taught.
--
-- Append-only, and the shape says so: no updated_at, no deleted_at, no soft
-- delete, and nothing but the retention job ever removes a row. The module
-- mounts no rest.Spec for the same reason — a Spec is five routes and three of
-- them write.
CREATE TABLE audit_events (
	id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id   uuid NOT NULL,
	-- When the state changed, which is when the outbox row was written, not
	-- when the worker got round to handling it.
	occurred_at timestamptz NOT NULL,
	name        text NOT NULL,
	-- The user whose request caused it, and NULL for a job, a handler or the
	-- bootstrap. platformkit_outbox.actor is where it comes from.
	actor       uuid,
	-- The outbox event's own id. It is what makes recording idempotent: the
	-- kernel's handled table already claims each delivery, and this is the
	-- second lock for the case the first cannot cover — an operator replaying a
	-- row, or a handler that failed after writing.
	event_id    uuid NOT NULL,
	payload     jsonb NOT NULL
);

CREATE UNIQUE INDEX audit_events_event ON audit_events (event_id);

-- The two questions the list route asks: the trail in reverse order, and the
-- trail for one kind of event. Both prefixed with tenant_id, because every
-- query runs under a policy that has already narrowed the table to one tenant.
CREATE INDEX audit_events_tenant_time ON audit_events (tenant_id, occurred_at DESC, id);
CREATE INDEX audit_events_tenant_name ON audit_events (tenant_id, name);

ALTER TABLE audit_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_events FORCE ROW LEVEL SECURITY;

CREATE POLICY audit_events_tenant ON audit_events
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
