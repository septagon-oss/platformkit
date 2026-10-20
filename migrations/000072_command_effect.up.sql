-- The command effect marker: one row per keyed command, naming the attempt that holds
-- its key and saying whether that attempt's transaction reached its COMMIT.
--
-- It exists for one moment in a command's life: the COMMIT went out and its answer
-- never came back (kit/db reports it as a failed Close). Nothing on this side of that
-- silence knows whether the write landed, and the claim row alone cannot say — the
-- answer it holds is written after the commit, by a request that can fail at exactly
-- that step. The claim therefore keeps the key and admits it holds no verdict; this
-- table is the verdict, because `committed` is set inside the command's own
-- transaction and so is TRUE in the database exactly when the command's effect is.
-- Rollback takes the flag with the work it takes; commit keeps both.
--
-- kit/idempotency writes the row, uncommitted, in the same system transaction that
-- hands an attempt the key, so the row always names the record's current claim. That
-- makes it the fence as well as the verdict: the command's transaction may set
-- `committed` only on a row naming its own attempt, so an attempt whose key was taken
-- over while it was still running finds a row naming somebody else, writes nothing,
-- and is rolled back. One key, one submission that can commit.
--
-- So the next request that arrives with that key asks this table rather than
-- guessing: `committed` on the row naming this attempt means the command ran and its
-- answer is gone, which is spent and never re-run. An existing unmarked fence
-- permits an identical retry within the operation replay window; a missing fence
-- proves nothing about legacy writes. That reading needs the marker's lock, which the writing transaction takes
-- before it sets the flag and keeps until it ends: a COMMIT still executing shows
-- neither flag nor work. Absence is evidence only across that lock — see
-- kit/idempotency's effectLockTakeSQL and resolveEffect.
--
-- The identity is the claim's own five columns, so one row answers for one record.
-- The tenant column is named tenant_id rather than tenant, as every scoped table names
-- it, because that name is what the shared policy predicate and the schema walk that
-- audits it are written against — this table is scoped, so it takes the scoped name.
--
-- Its policy is the shared one, which is the widening this file is about and is
-- stated plainly: unlike platformkit_idempotency, which no tenant transaction ever
-- touches, this table is written *by* the tenant's own transaction, because a flag
-- set outside the transaction it describes would say nothing about it. A tenant
-- transaction can therefore mark its own tenant's keys. What a forged mark can buy
-- its author is the kernel reading "this command committed" where it did not, which
-- spends a key it holds rather than freeing one: the direction a forged row points is
-- the safe one. Nothing is read out of these rows but the attempt they name and the
-- flag.
--
-- applied_at is the purge's clock and the only reason the column exists: the row of
-- a command remains while its retry identity is retained. An orphaned row expires
-- after a day. It is clock_timestamp() and not now(), as
-- in the outbox: a mark set by a long transaction would otherwise carry that
-- transaction's start, which is earlier than the commit that made it real.

CREATE TABLE platformkit_command_effect (
	tenant_id    uuid        NOT NULL,
	caller       uuid        NOT NULL,
	method       text        NOT NULL,
	route        text        NOT NULL,
	"key"        text        NOT NULL,
	-- The attempt that holds the key, matching platformkit_idempotency.claim.
	claim        uuid        NOT NULL,
	-- That attempt's transaction reached its COMMIT. Set only by that transaction.
	committed    boolean     NOT NULL DEFAULT false,
	applied_at   timestamptz NOT NULL DEFAULT clock_timestamp(),
	PRIMARY KEY (tenant_id, caller, method, route, "key")
);

-- The purge's query, and the only reader of the column.
CREATE INDEX platformkit_command_effect_applied
	ON platformkit_command_effect (applied_at);

ALTER TABLE platformkit_command_effect ENABLE ROW LEVEL SECURITY;
ALTER TABLE platformkit_command_effect FORCE ROW LEVEL SECURITY;

CREATE POLICY platformkit_command_effect_tenant ON platformkit_command_effect
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));

COMMENT ON TABLE platformkit_command_effect IS 'platformkit: one row per keyed command, naming the attempt that holds the key; committed is set inside that attempt''s own transaction by kit/idempotency at the kit/httpx request, and is the whole of what the row says';
