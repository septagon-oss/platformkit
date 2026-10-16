-- The delivery ledger: what happened to each channel a notice asked for.
--
-- A notice was a row and, for mail, an outbox request; what became of the request
-- — handed to the relay, or never sent because the recipient has no address or the
-- notice was deleted first — was visible nowhere. Every channel a notice asks for
-- gets a `requested` row when the notice is written and a terminal row when the
-- channel finishes, in the same transaction as the step that finished it, so
-- "did Ada get told" is a query and not a support ticket. `delivery_ledger_coverage`
-- is requested channels with a terminal row, over requested channels.
--
-- Append-only: a delivery is a fact about a moment, so nothing here is updated or
-- deleted by the application, and the audit trail is the ledger itself.
CREATE TABLE notification_deliveries (
	id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	-- The order the rows were appended in. now() is the transaction's start, so every row a
	-- step writes would share one timestamp; a sequence is the ledger's own order.
	seq             bigint GENERATED ALWAYS AS IDENTITY,
	tenant_id       uuid NOT NULL,
	-- The moment of the step itself, not of its transaction's start.
	created_at      timestamptz NOT NULL DEFAULT clock_timestamp(),
	notification_id uuid NOT NULL,
	-- in_app today, email; push and webhook arrive as providers behind the same port.
	channel         text NOT NULL CHECK (channel IN ('in_app', 'email', 'push', 'webhook')),
	-- requested when the notice asks for the channel; sent or suppressed when it ends.
	outcome         text NOT NULL CHECK (outcome IN ('requested', 'sent', 'suppressed', 'failed')),
	-- Why a channel was suppressed or failed, in words an operator reads; empty when sent.
	reason          text NOT NULL DEFAULT ''
);

CREATE INDEX notification_deliveries_notice ON notification_deliveries (tenant_id, notification_id, seq);

ALTER TABLE notification_deliveries ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_deliveries FORCE ROW LEVEL SECURITY;

CREATE POLICY notification_deliveries_tenant ON notification_deliveries
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
