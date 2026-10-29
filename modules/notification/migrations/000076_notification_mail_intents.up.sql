-- The mail this module owes: one row per mail a caller decided to send, written in
-- the transaction of that decision and sent by a worker after it commits.
--
-- Owner: notification (000076). It is a third subject, not a wider copy of the two
-- ledgers beside it: notification_deliveries (000027) is one channel of one notice
-- keyed by notification_id and is what delivery_ledger_coverage counts, and
-- direct_mail_deliveries (000044) is written once, at the moment a send is decided,
-- with no first half to be pending about. Neither can hold a row that begins before
-- the send is decided — this one does, which is the whole reason it exists: a mail
-- whose relay was dialled inside the caller's transaction was a mail that could be
-- sent by a transaction that rolled back, and a mail whose answer was lost left no
-- trace at all.
--
-- Both ledgers stay exactly what their names say. The terminal row of a notice's
-- email channel is now written by the worker's outcome transaction instead of by
-- the handler, and an intent that ends `unknown` writes no row in either ledger:
-- an unresolved send lives here and nowhere else, because a ledger row says at most
-- what this process can prove.
--
-- One row per intent, not one per attempt: the attempt is a moment and the intent is
-- the duty, and a table that appended a row per socket would answer "is this mail
-- still owed" by counting rows that all say yes. `attempt` and `uncertain` are the
-- two counters, and the delivery ledgers remain the log.
--
-- Nothing backfills: the table begins empty, every row in it is written by this
-- module, and there is no historical mail to migrate because the mail this replaces
-- was never recorded before it was sent.
CREATE TABLE notification_mail_intents (
	id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	-- The order the rows were appended in, so a claim is fair and the retention
	-- sweep has a tie-break: clock_timestamp() at one transaction's boundary
	-- cannot order the rows that transaction wrote.
	seq            bigint GENERATED ALWAYS AS IDENTITY,
	tenant_id      uuid NOT NULL,
	created_at     timestamptz NOT NULL DEFAULT clock_timestamp(),
	-- Which mail this is, in the grammar every event name uses. No list of purposes
	-- lives here, for 000044's reason: which mails a product mails is the product's
	-- fact, and a CHECK that enumerated them would be a schema change per mail.
	purpose        text NOT NULL CHECK (purpose ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$'),
	-- The stable identity of whatever asked for the mail: the source event id, or a
	-- caller-owned key stable across its own retries. Empty is the caller claiming
	-- no identity, and such a mail is never deduplicated — see the unique index.
	cause          text NOT NULL DEFAULT '' CHECK (length(cause) <= 200),
	-- Who the mail is about and which notice it belongs to. No foreign keys: an FK
	-- would make a notice deletion a blocked deletion, and the link is read at send
	-- time, where its absence is an answer (suppressed) rather than a constraint.
	person_id      uuid NOT NULL,
	notification_id uuid NOT NULL,
	-- The address the mail was decided for, spelled by the caller through
	-- contracts.EmailKey and never re-normalised a second time here. The worker
	-- re-reads the person's address before the socket opens and suppresses the mail
	-- if it moved; it never rewrites this column.
	recipient      text NOT NULL CHECK (recipient <> '' AND length(recipient) <= 320),
	subject        text NOT NULL DEFAULT '',
	body           text NOT NULL CHECK (body <> ''),
	html           text NOT NULL DEFAULT '',
	lang           text NOT NULL DEFAULT '',
	reply_to       text NOT NULL DEFAULT '',
	-- The six states, the same six contracts spells in Go. in_flight with an expired
	-- lease becomes unknown, never re-sent: see MailIntentUnknownDelay.
	state          text NOT NULL CHECK (state IN ('queued', 'in_flight', 'sent', 'failed', 'suppressed', 'unknown')),
	-- How many transports this intent was handed. Only a claim raises it, so a
	-- crash cannot inflate it without a socket opening.
	attempt        int NOT NULL DEFAULT 0 CHECK (attempt >= 0),
	-- How many of those hand-offs came back with no answer. This is the counter that
	-- makes the unknown rule a rule instead of an accident of the lease.
	uncertain      int NOT NULL DEFAULT 0 CHECK (uncertain >= 0),
	-- When a queued, or re-armable unknown, row may be sent.
	available_at   timestamptz NOT NULL DEFAULT clock_timestamp(),
	-- When this attempt was claimed: the lease's origin, and the only timestamp that
	-- can bound "the process died across the socket".
	attempted_at   timestamptz,
	updated_at     timestamptz NOT NULL DEFAULT clock_timestamp(),
	-- The last answer, in words an operator reads. Every state except sent and the
	-- first claim of a fresh row has one, which is the rule 000044's column already
	-- makes a CHECK enforce over there.
	reason         text NOT NULL DEFAULT ''
);

-- The drain's shape: partial, so the index is the size of the pending clock and not
-- the size of the history. It covers the three states a claim reads — a terminal row
-- is never due, and an unknown that has spent its re-send is not due either.
CREATE INDEX notification_mail_intents_pending ON notification_mail_intents (tenant_id, available_at, seq)
	WHERE state IN ('queued', 'in_flight', 'unknown');

-- One intent per (purpose, cause): a redelivered event or a replayed one cannot add a
-- second mail, and two concurrent writers cannot both win. This is the answer to the
-- check-then-insert race, which is the same bug 000056 exists for on the sent row.
-- A cause of '' names no identity and is excluded on purpose: an index that treated
-- the empty string as a key would silently drop every mail after the first that
-- declined to claim one.
CREATE UNIQUE INDEX notification_mail_intents_one ON notification_mail_intents (tenant_id, purpose, cause)
	WHERE cause <> '';
CREATE INDEX notification_mail_intents_person ON notification_mail_intents (tenant_id, person_id, seq);

ALTER TABLE notification_mail_intents ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_mail_intents FORCE ROW LEVEL SECURITY;

CREATE POLICY notification_mail_intents_tenant ON notification_mail_intents
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
