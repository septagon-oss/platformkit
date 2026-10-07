-- The command effect marker: one row saying "the transaction that ran this keyed
-- command reached its COMMIT", written inside that transaction.
--
-- It exists for one moment in a command's life: the COMMIT went out and its answer
-- never came back (kit/db's ErrCommitUnacknowledged). Nothing on this side of that
-- silence knows whether the write landed, and the claim row alone cannot say — the
-- answer it holds is written after the commit, detached, by a request that can fail
-- at exactly that step. The claim therefore keeps the key and admits it holds no
-- verdict; this table is the verdict, and it is the only statement in the kernel
-- that can make one, because a row written inside the transaction is in the database
-- exactly when the command's effect is and nowhere when it is not. Rollback takes
-- the marker with the work it takes; commit keeps both.
--
-- So the next request that arrives with that key asks this table rather than
-- guessing: a marker here means the command ran and the answer the claim holds is
-- the answer to it; no marker means the transaction that wrote the claim is not in
-- the database, and the identical retry may run.
--
-- The row names its tenant, its caller and its route for the same reason the claim
-- does — one key must not speak for two customers or two accounts — and its policy
-- is the shared one, which is the widening this file is about and is stated plainly:
-- unlike platformkit_idempotency, which no tenant transaction ever touches, this
-- table is written *by* the tenant's own transaction, because a marker written
-- outside the transaction it describes would say nothing about it. A tenant
-- transaction can therefore place a marker for its own tenant's keys. What a marker
-- can buy its author is the kernel reading "this command committed" where it did
-- not, which spends a key it holds rather than freeing one: the direction a forged
-- row points is the safe one. Nothing is read out of these rows but whether one is
-- there, and the submission it names.
--
-- applied_at is the purge's clock and the only reason the column exists: the marker
-- of a command is worth exactly as much as the claim that answers for it, which is
-- the day docs/adr/0016 gives the key. It is clock_timestamp() and not now(), as in
-- the outbox: a marker written by a long transaction would otherwise carry that
-- transaction's start, which is earlier than the commit that made it real.

CREATE TABLE platformkit_command_effect (
	tenant_id  uuid        NOT NULL,
	actor_id   uuid        NOT NULL,
	operation  text        NOT NULL,
	key        text        NOT NULL,
	-- Which submission this marker is the effect of. The claim names the same id, and
	-- a marker left by an older submission under one key says nothing about a later
	-- one, so the resolution asks for the id and not for the key alone.
	request_id text        NOT NULL DEFAULT '',
	applied_at timestamptz NOT NULL DEFAULT clock_timestamp(),
	PRIMARY KEY (tenant_id, actor_id, operation, key)
);

-- The purge's query, and the only reader of the column.
CREATE INDEX platformkit_command_effect_applied
	ON platformkit_command_effect (applied_at);

ALTER TABLE platformkit_command_effect ENABLE ROW LEVEL SECURITY;
ALTER TABLE platformkit_command_effect FORCE ROW LEVEL SECURITY;

CREATE POLICY platformkit_command_effect_tenant ON platformkit_command_effect
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));

COMMENT ON TABLE platformkit_command_effect IS 'platformkit: one row per keyed command whose transaction committed, written inside that transaction by kit/httpx; its existence is the whole of what it says';

-- The claim's side of the same arrangement: the answer below is held against a
-- COMMIT nobody read, and is owed to nobody until the marker above says which way
-- that COMMIT went. NULL DEFAULT false keeps every row an installation already has:
-- a row written before this file settled an answer the kernel saw land, and there
-- is nothing to resolve about it.
ALTER TABLE platformkit_idempotency
	ADD COLUMN unconfirmed boolean NOT NULL DEFAULT false;

COMMENT ON COLUMN platformkit_idempotency.unconfirmed IS 'platformkit: the response below was written by a request whose COMMIT was never acknowledged; the answer is held, not owed, until platformkit_command_effect says whether that commit landed. kit/httpx/idempotency.go resolves it on the next press with the same key.';
