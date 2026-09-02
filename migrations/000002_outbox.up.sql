-- The transactional outbox: the one door a module's events leave through.
--
-- A module writes a row here in the same transaction as the state change that
-- caused it (kit/events.Publish), so the event is exactly as durable as the
-- change and neither can exist without the other. The relay in the worker role
-- reads unpublished rows, hands each to the transport and stamps published_at.
-- Delivery is at-least-once by construction: the relay can publish and then
-- fail before the stamp commits, so handlers deduplicate on the event id.
--
-- The table is tenant-owned like any other, with the shape 000001 describes:
-- ENABLE plus FORCE row-level security and the shared predicate. The relay
-- crosses tenants deliberately, through db.RunSystem, which is the one thing
-- platformkit_tenant_match lets past.

CREATE TABLE platformkit_outbox (
	id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id    uuid NOT NULL,
	name         text NOT NULL,
	payload      jsonb NOT NULL,
	-- clock_timestamp() and not now(): now() is the transaction's start time, so
	-- every event one transaction publishes would carry the same timestamp and
	-- the relay's ORDER BY created_at, id would order them by a random uuid.
	created_at   timestamptz NOT NULL DEFAULT clock_timestamp(),
	published_at timestamptz
);

-- The relay's query is the only reader of unpublished rows, and it wants them
-- oldest first. A partial index keeps the queue's index the size of the queue
-- rather than the size of the history.
CREATE INDEX platformkit_outbox_pending
	ON platformkit_outbox (created_at, id) WHERE published_at IS NULL;

-- The purge's query, for the same reason in the opposite direction.
CREATE INDEX platformkit_outbox_published
	ON platformkit_outbox (published_at) WHERE published_at IS NOT NULL;

ALTER TABLE platformkit_outbox ENABLE ROW LEVEL SECURITY;
ALTER TABLE platformkit_outbox FORCE ROW LEVEL SECURITY;

CREATE POLICY platformkit_outbox_tenant ON platformkit_outbox
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
