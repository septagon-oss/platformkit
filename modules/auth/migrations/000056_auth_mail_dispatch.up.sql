-- The credential mail this module decided to send: one row per message, written in the
-- transaction that minted the link it names, and marked with whatever the transport
-- answered afterwards.
--
-- Owner: auth (000056). It is a third subject, not a wider copy of the two ledgers
-- beside it. notification_deliveries (000027) is one channel of one notice, and
-- direct_mail_deliveries (000044) is written once, at the moment a send is decided, with
-- no first half to be pending about. Neither can hold a row that begins before the send
-- is decided, and this table's whole subject is that window: the credential commits, the
-- message is handed over after that commit, and a process that stops in between leaves a
-- link nobody was sent with no trace anywhere. The state column is where that becomes
-- something an operator can read rather than something they infer from a complaint.
--
-- The credential is here only as its hash, for the reason modules/auth/000014 gives at
-- length: a reset link is a door, and a table that holds doors is a table a dump of is an
-- account. What the row needs in order to be marked is sha256 of the token, which is what
-- the credential tables themselves hold and unusable on its own. Nothing in this file can
-- hold a subject, a body, a link or a transport's words, and that absence is the design:
-- the words a transport said have exactly one home in this repository,
-- direct_mail_deliveries.reason, written through RedactMailReason.
--
-- No request id and no traceparent, though the ledger carries both: the two records answer
-- different questions and are not joined. The public delivery door's defence is that a
-- caller is answered from a record its own call caused; a read of this table keyed by a
-- request id or an address would be an account enumeration oracle with a schema behind it.
--
-- No foreign key to password_tokens or verification_tokens, the way 000076 refuses its
-- own. A credential row is deleted by spending it, by a refused send putting the previous
-- link back, and by a person's erasure, and this evidence has to outlive all three — a
-- key that died with the row it describes would record only the sends that went well.
--
-- Nothing backfills: the table begins empty and every row in it is written by this module.
-- An unsettled row outlives every sweep this module runs; see the retention statement in
-- Purge for why that is the point rather than an oversight.
CREATE TABLE auth_mail_dispatch (
	tenant_id   uuid   NOT NULL,
	user_id     uuid   NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	-- sha256 of the token this message carried. The primary key is what makes "one row
	-- per decided message" a database fact: the token is 256 bits of crypto/rand, so one
	-- hash is one attempt, and a retry mints a hash of its own.
	token_hash  bytea  NOT NULL,
	-- Which mail this is, in the grammar every event name uses. No list of kinds lives
	-- here, for 000044's reason: which mails a module mails is that module's fact, and a
	-- CHECK that enumerated them would be a schema change per mail.
	kind        text   NOT NULL CHECK (kind ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$'),
	-- The address the message was decided for, spelled by the caller through
	-- contracts.EmailKey and never re-normalised a second time here. It is stored rather
	-- than joined because the ledger it would be joined to may not exist: a composition
	-- that wires no MailLedger records no mail, and this row still has to describe itself.
	recipient   text   NOT NULL CHECK (recipient <> '' AND length(recipient) <= 320),
	-- unsent: committed, handed over, no fate recorded. Not a claim the message never
	-- left — a hand-off may be open now, or may have died across the socket.
	-- sent / withdrawn: the transport's answer, committed beside the send's own marks.
	-- unknown: nobody holds the secret any more and no fate was ever recorded. Nothing
	-- may move it to sent, because nothing can prove the mail left.
	state       text   NOT NULL CHECK (state IN ('unsent', 'sent', 'withdrawn', 'unknown')),
	created_at  timestamptz NOT NULL DEFAULT clock_timestamp(),
	-- When this row stopped being something that could still change. NULL is the state
	-- this table exists to make visible: nobody recorded this send's fate.
	settled_at  timestamptz,
	PRIMARY KEY (tenant_id, token_hash)
);

-- What the two sweeps walk: unsettled rows by age, for the grace that turns a lost
-- hand-off into `unknown`, and settled rows by the moment they became forgettable.
CREATE INDEX auth_mail_dispatch_unsettled ON auth_mail_dispatch (tenant_id, created_at)
	WHERE settled_at IS NULL;
CREATE INDEX auth_mail_dispatch_settled ON auth_mail_dispatch (tenant_id, settled_at)
	WHERE settled_at IS NOT NULL;

ALTER TABLE auth_mail_dispatch ENABLE ROW LEVEL SECURITY;
ALTER TABLE auth_mail_dispatch FORCE ROW LEVEL SECURITY;

CREATE POLICY auth_mail_dispatch_tenant ON auth_mail_dispatch
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
