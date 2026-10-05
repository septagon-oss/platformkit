-- A claim at an unscoped durable declares that durable busy for its transaction.
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
-- Keyed on the durable and on nothing else, because that is the whole boundary of
-- the harm. A deployment that has moved pays this nothing: the WHEN clause below is
-- the same predicate as the move's, so a claim under a scoped durable takes no lock,
-- and the steady state after the flip is the state before the file. It is also why
-- a table lock in the move is the wrong shape: two apps share these tables, and a
-- lock that conflicts with every INSERT lets one app's ordinary traffic refuse the
-- other app's move forever, which leaves the window open while both apps' consumers
-- run — the harm the move exists to close, dressed up as a safety measure.
--
-- The key is the durable's own text hashed into the 64-bit advisory space, the same
-- call kit/db and modules/auth use for their keys. Two durables whose hashes
-- collide answer with a refusal that wrote nothing and is retried: the direction a
-- hash collision can be wrong in here is the cheap one.
CREATE FUNCTION platformkit_ledger_claim_lock() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
	-- The same key the move asks for: kit/events/ledger.go's durableLock hashes the
	-- same text, and the two must not be able to drift or the pair excludes nothing.
	PERFORM pg_advisory_xact_lock_shared(hashtextextended('events ledger ' || NEW.durable, 0));
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
	'platformkit: takes the shared advisory lock of the durable an unscoped claim is '
	'written under, so kit/events.MoveLedger can refuse a rename under an open claim '
	'instead of taking the whole table; see the header of migrations/000044';
