-- The record of a mail that left this process with no notification row behind it.
--
-- notification_deliveries (000027) is the ledger of one channel of one notice: it begins at
-- `requested` and is keyed by notification_id. The mails this table records are the ones a
-- composition sends outside a notice — a set-password link and a verification link, minted by
-- modules/auth and handed to contracts.Mailer directly, so the secret is in the message and in no
-- row (modules/notification/contracts/notification.go). What they left behind was nothing: no row
-- said a mail was attempted, to whom, whether SMTP took it, or under which call. One row per send,
-- appended in the transaction of the step that decided it, saying who it went to, which kind it
-- was, what became of it and which call asked.
--
-- It holds no subject, no body, no link and no credential: a mail that carried a set-password
-- token is recorded as having been sent to an address, and that is the whole of what is written
-- down. The reason column carries what the transport said, redacted and bounded by
-- contracts.RedactMailReason, because a transport may quote its input in an error (see
-- modules/auth/internal/verification.go).
--
-- Append-only: nothing here is updated or deleted by the application. A row says at most what this
-- process can prove; a `sent` row whose transaction aborted is a mail nobody recorded, which is the
-- direction this table would rather be wrong in.
CREATE TABLE direct_mail_deliveries (
	id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	-- Ledger order, as in 000027: now() is the transaction's start, so rows one step writes
	-- would share one timestamp.
	seq         bigint GENERATED ALWAYS AS IDENTITY,
	tenant_id   uuid NOT NULL,
	-- The moment of the step itself, not of its transaction's start.
	created_at  timestamptz NOT NULL DEFAULT clock_timestamp(),
	-- Who the mail is named by. A mail with no notice has no recipient column to point at, so the
	-- address is the only name it has. contracts.EmailKey spells it; nothing here re-normalises it.
	recipient   text NOT NULL CHECK (recipient <> '' AND length(recipient) <= 320),
	-- Which mail, in the grammar every event name uses (kit/events/transport ValidName): today
	-- auth.set_password and auth.verification. Open on purpose — a CHECK listing kinds would make
	-- the next module that mails a schema change.
	kind        text NOT NULL CHECK (kind ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$'),
	-- sent when the transport took it; failed when it refused; suppressed when there was nothing to
	-- send it through. No `requested`: this row is written once, at the decision, not twice.
	outcome     text NOT NULL CHECK (outcome IN ('sent', 'suppressed', 'failed')),
	-- Why it did not go, empty when it did. The CHECK is the table's share of the promise: an
	-- outcome that is not `sent` has to say why.
	reason      text NOT NULL DEFAULT '' CHECK (length(reason) <= 500 AND (outcome = 'sent' OR reason <> '')),
	-- Which call caused this, as the call was answered it. NULL is ordinary: a job's mail, a mail
	-- with no call behind it (migrations/000028, 000034, and kit/events.Publish's nilIfEmpty).
	traceparent text,
	request_id  text CHECK (request_id IS NULL OR length(request_id) <= 64)
);

-- The one read this table has: the newest outcome recorded for one request id. An index the
-- application never queries would be a cost with no reader, so only this one exists.
CREATE INDEX direct_mail_deliveries_request ON direct_mail_deliveries (tenant_id, request_id, seq DESC);

ALTER TABLE direct_mail_deliveries ENABLE ROW LEVEL SECURITY;
ALTER TABLE direct_mail_deliveries FORCE ROW LEVEL SECURITY;

CREATE POLICY direct_mail_deliveries_tenant ON direct_mail_deliveries
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
