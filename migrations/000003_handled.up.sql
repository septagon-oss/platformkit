-- The handled ledger: what makes at-least-once delivery run a handler once.
--
-- The relay publishes and then stamps published_at, because the other order
-- loses events (docs/adr/0004), so every subscription sees some events twice.
-- kit/events.Consume claims each delivery here, inside the handler's own
-- transaction, before the handler runs: the claim and the handler's writes
-- commit together, so a handler that fails leaves no claim and comes back, and
-- a handler that succeeded is never run again.
--
-- The key is (event_id, durable) and not event_id alone: two subscriptions to
-- one event are two pieces of work, and each has to do its own.
--
-- The rows are tenant-owned like everything else, with the shape 000001
-- describes. The tenant is the event's, which is the tenant the handler's
-- transaction is scoped to, so the claim is visible to exactly the transactions
-- that could contend for it.

CREATE TABLE platformkit_handled (
	event_id   uuid NOT NULL,
	durable    text NOT NULL,
	tenant_id  uuid NOT NULL,
	handled_at timestamptz NOT NULL DEFAULT now(),
	PRIMARY KEY (event_id, durable)
);

-- The purge's query: oldest first, the same week-long window the outbox keeps.
CREATE INDEX platformkit_handled_handled_at ON platformkit_handled (handled_at);

ALTER TABLE platformkit_handled ENABLE ROW LEVEL SECURITY;
ALTER TABLE platformkit_handled FORCE ROW LEVEL SECURITY;

CREATE POLICY platformkit_handled_tenant ON platformkit_handled
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
