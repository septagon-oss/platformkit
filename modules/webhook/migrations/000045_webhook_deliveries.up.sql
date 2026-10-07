-- The delivery log: one row per attempt to deliver one event to one endpoint.
--
-- Owner: webhook. The shape is modules/notification's 000027 — a bigint identity
-- for the log's own order, clock_timestamp() for the moment of the row rather
-- than its transaction's start, row-level security FORCEd — and its subject is
-- different on purpose: notification_deliveries answers "was this person told",
-- over channels whose subject is a person. This answers "did this event leave for
-- that URL, and what did they answer", over rows whose subject is a machine
-- address a tenant registered. Neither table is a copy of the other.
--
-- One row per attempt, not one row per event: a retry is a different moment, a
-- different socket and a different answer, and a log that overwrote its own rows
-- would answer "was there a 500" with "no" because the last attempt succeeded.
--
-- payload is jsonb and is written once, at fan-out: the bytes the signature is
-- computed over are the bytes POSTed, so a retry signs exactly what the first
-- attempt signed. Nothing updates this column. The database may reformat an
-- out-of-range number inside jsonb; the module signs what it read back, which is
-- why the sender sends the stored document and not a fresh marshal of the event.
CREATE TABLE webhook_deliveries (
	id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	-- The order the rows were appended in. now() is the transaction's start, so
	-- every row one step writes would share a timestamp.
	seq            bigint GENERATED ALWAYS AS IDENTITY,
	tenant_id      uuid NOT NULL,
	-- When the row was appended, which for a queued attempt is when the event
	-- arrived and for a retried one is when the ladder wrote it.
	created_at     timestamptz NOT NULL DEFAULT clock_timestamp(),
	endpoint_id    uuid NOT NULL,
	-- The CloudEvent id, which is also the webhook-id header a receiver
	-- deduplicates on.
	event_id       uuid NOT NULL,
	-- What matched, so the log answers "why did I get this" without a join.
	event_name     text NOT NULL,
	payload        jsonb NOT NULL,
	-- 1 for the first attempt. The ladder's ordinal, not a count of sockets: a
	-- row re-sent after its lease expired keeps its number and its payload.
	attempt        int NOT NULL DEFAULT 1 CHECK (attempt > 0),
	-- Why the row exists. 'retry' is the platform's decision, 'replay' a person's,
	-- and the log has to tell them apart.
	cause          text NOT NULL CHECK (cause IN ('initial', 'retry', 'replay')),
	state          text NOT NULL CHECK (state IN ('queued', 'in_flight', 'sent', 'failed', 'dropped')),
	-- When a queued row may be sent: the ladder's next rung.
	available_at   timestamptz NOT NULL DEFAULT clock_timestamp(),
	attempted_at   timestamptz,
	-- The moment the row reached its terminal state — the log's own updated_at.
	updated_at     timestamptz NOT NULL DEFAULT clock_timestamp(),
	deleted_at     timestamptz,
	-- 0 when nothing answered: no connection, no handshake, no lease honoured.
	status_code    int NOT NULL DEFAULT 0,
	-- The refusal or the failure in words an administrator reads.
	reason         text NOT NULL DEFAULT ''
);

-- The drain's own shape: partial, because the index is the size of the pending
-- clock and not the size of the history.
CREATE INDEX webhook_deliveries_pending ON webhook_deliveries (tenant_id, available_at, seq)
	WHERE state = 'queued';
CREATE INDEX webhook_deliveries_endpoint ON webhook_deliveries (tenant_id, endpoint_id, seq);
CREATE INDEX webhook_deliveries_event ON webhook_deliveries (tenant_id, event_id);

-- Fan-out is exactly-once per (event, endpoint): the claim table says one event
-- is handled once per subscription, and this says one queued row per pair, which
-- also covers a handler that failed after writing and an outbox row an operator
-- replayed.
CREATE UNIQUE INDEX webhook_deliveries_initial ON webhook_deliveries (event_id, endpoint_id)
	WHERE cause = 'initial';
-- A second replay of one pair writes nothing and answers with the row that is
-- already there.
CREATE UNIQUE INDEX webhook_deliveries_replay ON webhook_deliveries (event_id, endpoint_id)
	WHERE cause = 'replay';

ALTER TABLE webhook_deliveries ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_deliveries FORCE ROW LEVEL SECURITY;

CREATE POLICY webhook_deliveries_tenant ON webhook_deliveries
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
