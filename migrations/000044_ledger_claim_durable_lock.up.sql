-- A claim at an unscoped durable declares that durable, and its own tenant, busy
-- for its transaction.
--
-- kit/events/ledger.go renames the two delivery ledgers' unscoped rows onto the
-- durable of the app that holds their tenant. Its one hazard is a claim written
-- under one of those names while the rename runs: the copy takes a snapshot of the
-- rows that exist, the delete then removes that set, and a mark that committed in
-- between is gone — the claim that stops a handler running twice, taken away by the
-- move that exists to stop handlers running twice.
--
-- The move therefore takes a transaction-scoped exclusive lock per durable it is
-- about to rename, and refuses rather than waits. A refusal is the correct answer to
-- a delivery in flight and it writes nothing; a queue would be the same answer late.
-- What makes the pair work is the *other* half, which has to be taken by every
-- writer of a claim and not only by the one Go function that writes them today: an
-- INSERT of a row whose durable names no app takes the matching transaction-scoped
-- *shared* advisory lock. Every claim then declares its durable busy until its own
-- transaction ends — which is exactly the lifetime of the insert lock the claim
-- already holds — and any number of concurrent claims under one durable take it
-- together, because shared advisory locks do not exclude each other.
--
-- Keyed on the durable and on the tenant, and on nothing else, because those two are
-- the whole boundary of the harm. Keyed on the durable *alone* was not enough, and
-- the first claim of a subscription is what says so: kit/events names the durables
-- to lock by reading committed ledger rows, so a subscription that has never
-- committed a claim names nothing to lock, the move finds nothing to rename and
-- answers its zero report as a success while that handler is still running, and the
-- claim then commits its mark under the unscoped name a moment later — invisible to
-- the scoped consumer, which is the second handling this file exists to prevent. The
-- durable is the thing being discovered, so it cannot be the whole question. What
-- every claim names, whatever it claims, is the tenant it was taken in, and the
-- placement gives a tenant exactly one app (tenants.app, 000043), so the tenant's key
-- catches the claim no committed row has named yet. It is still not a wider lock over
-- the other app: academy's delivery holds academy's tenant's key, which collect's
-- move never asks for, so one app's ordinary traffic refuses the other app's boot
-- exactly as often as before — the harm a table lock would dress up as a safety
-- measure stands off by the same margin.
--
-- A deployment that has moved pays this nothing: the WHEN clause below is the same
-- predicate as the move's, so a claim under a scoped durable takes no lock at all,
-- and the steady state after the flip is the state before the file. The file keeps
-- its name while it takes a second key: the ledger of any database that applied this
-- version records the pair of version and name, and the pair is what must not move.
--
-- The keys are the durable's and the tenant's own text hashed into the 64-bit
-- advisory space, the same call kit/db and modules/auth use for their keys. Two
-- durables whose hashes collide answer with a refusal that wrote nothing and is
-- retried: the direction a hash collision can be wrong in here is the cheap one.
CREATE FUNCTION platformkit_ledger_claim_lock() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
	-- The same key the move asks for: kit/events/ledger.go's durableLock hashes the
	-- same text, and the two must not be able to drift or the pair excludes nothing.
	PERFORM pg_advisory_xact_lock_shared(hashtextextended('events ledger ' || NEW.durable, 0));
	-- And the tenant's, for the claim whose durable nothing has committed under yet,
	-- which the line above cannot name because naming it is what the move is doing.
	-- kit/events/ledger.go's tenantLockKey carries this exact expression.
	PERFORM pg_advisory_xact_lock_shared(hashtextextended('events ledger tenant ' || NEW.tenant_id::text, 0));
	RETURN NEW;
END
$$;

CREATE TRIGGER platformkit_handled_claim_lock
	BEFORE INSERT ON platformkit_handled
	FOR EACH ROW WHEN (strpos(NEW.durable, '+') = 0)
	EXECUTE FUNCTION platformkit_ledger_claim_lock();

CREATE TRIGGER platformkit_dead_letters_claim_lock
	BEFORE INSERT ON platformkit_dead_letters
	FOR EACH ROW WHEN (strpos(NEW.durable, '+') = 0)
	EXECUTE FUNCTION platformkit_ledger_claim_lock();

COMMENT ON FUNCTION platformkit_ledger_claim_lock() IS
	'platformkit: takes the shared advisory locks of the durable and of the tenant an unscoped claim is '
	'written under, so kit/events.MoveLedger can refuse a rename under an open claim — the first claim of a '
	'subscription included, whose durable no committed row names — instead of taking the whole table; see the '
	'header of migrations/000044';
