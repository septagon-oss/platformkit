-- The dead-letter ledger: events a subscription could not handle, ever.
--
-- Delivery is at-least-once and a failed handler is a negative acknowledgement,
-- so a poison event — one that fails for a reason no retry can fix — would come
-- back forever, and the transport would spend the worker on it. Both transports
-- therefore give up after events.maxDeliveries attempts: the message is
-- terminated and one row lands here, with the last error.
--
-- A row here is an alert, not a queue. Nothing redelivers from this table; it
-- exists so that "the handler never ran" is a question with an answer, and the
-- outbox still holds the event for as long as the purge keeps it.
--
-- Tenant-owned like everything else, with the shape 000001 describes. The
-- writer is db.RunSystem rather than db.Run, because the reason a delivery
-- failed may be that the tenant transaction could not be opened at all.

CREATE TABLE platformkit_dead_letters (
	event_id  uuid NOT NULL,
	durable   text NOT NULL,
	tenant_id uuid NOT NULL,
	name      text NOT NULL,
	error     text NOT NULL,
	failed_at timestamptz NOT NULL DEFAULT now(),
	PRIMARY KEY (event_id, durable)
);

-- An operator's query: what has died lately.
CREATE INDEX platformkit_dead_letters_failed_at ON platformkit_dead_letters (failed_at DESC);

ALTER TABLE platformkit_dead_letters ENABLE ROW LEVEL SECURITY;
ALTER TABLE platformkit_dead_letters FORCE ROW LEVEL SECURITY;

CREATE POLICY platformkit_dead_letters_tenant ON platformkit_dead_letters
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
