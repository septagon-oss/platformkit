-- The write that names a tenant's app declares that tenant's ledger busy.
--
-- migrations/000044 gives the two delivery ledgers one pair of keys: a claim takes
-- the shared advisory lock of its tenant and then of its durable, and
-- kit/events.MoveLedger asks for both exclusively, refusing rather than queueing.
-- That pair is why a move can rename a ledger without taking the table — and it has
-- one hole, which is that the claim is not the only writer the move has to respect.
--
-- The move's set is `app = this app OR app = ''` (kit/events/ledger.go's `placeable`):
-- every tenant that could still be its app by the time it commits. It holds each of
-- those keys, reads the durables its tenants name, renames them, and asks once more —
-- `unscopedRemains` — whether any unscoped row now sits in one of its tenants. That
-- last question is the one that has to be honest, and until this file it was not,
-- because the placement walked in beside it. `platformkit_place_tenants` (000045)
-- wrote `tenants.app` under no key at all: the row lock it takes serializes it only
-- against another placement, and a move holds no row lock. So a second declaring boot
-- — or the same boot's drain, or the release file's own window — could take an empty
-- tenant, with its committed unscoped claims, while the move of the app it was joining
-- sat past its final read. The read answered false because the row was still empty; the
-- move renamed the tenants it had seen, published its record, committed, and released
-- the keys it never held for that tenant. The tenant's own scoped consumer then looked
-- for (event_id, "collect+mod-ev"), found nothing — its claim is sitting under
-- "mod-ev" — and ran a handler for work its tenant had already committed. The scheduled
-- later move cannot undo that: by the time it renames the row, the effect has committed
-- twice.
--
-- The move cannot see this coming, and it does not have to. The claim has the same
-- problem and answers it the same way: `holdsUnscoped` declares the tenant *before* it
-- reads, because "a lock taken after a decision records it; taken before, it makes it
-- authoritative". A placement is a decision about one tenant's app, taken in a
-- transaction that ends by naming it, so it takes the same key the claim takes, in the
-- same order — before the write, held to commit. A placement that arrives while a move
-- holds the key waits: the only holder is that move's own transaction, which renames or
-- refuses and ends, and the placement then lands behind it and owes the move its own
-- boot runs next. A placement that arrives first is the move's ordinary refusal — the
-- tenant is in its set either way, and the refusal writes nothing, emits nothing, and
-- says "run it again". Either order leaves no tenant inside an app whose move has
-- already reported, with a ledger the move never saw.
--
-- Why a trigger, and not a statement inside `platformkit_place_tenants`. The function
-- decides *which* tenants to name by reading a snapshot it may lose to a competing
-- walk (000045 states that race and puts its check in the write, `t.app = ''`); a lock
-- taken over that reading would be taken over the set the reading believed, and the
-- set that matters is the set the statement actually wrote — which is the same set only
-- when nothing raced. A row trigger fires on the row the write reached, so the two
-- cannot disagree, and it fires on the row that *changed*: a placement that re-states an
-- app already there, or finds the row already named by the walk that beat it, is a
-- placement with nothing to declare and takes nothing. The cost is one advisory lock
-- entry per tenant this transaction actually moves onto an app, beside the row lock that
-- same write already takes — not one per tenant it merely considered, which over a
-- whole-table placement would have been the difference between a walk and an out-of-shared-memory
-- failure.
--
-- It is an UPDATE trigger, and an INSERT is not in it. A tenant created with an app
-- (modules/tenant stamps the create from the composition's slug, 000043's default is the
-- floor under a raw one) arrives with no ledger: nothing was ever handled or terminated
-- in it under an unscoped name, so there is no row for a move to have missed, and the
-- first claim it ever writes declares its tenant for itself. The empty-to-slug step on a
-- row that may already hold unscoped rows is the write this key is for, and it is one
-- statement, one row, and one lock.
--
-- The key is 000044's, spelled the same way: `events ledger tenant ` || id, hashed by
-- the same call, so the placement that asks it and the claim that asks it and the move
-- that refuses them are one key and not three that happen to collide. A placement of
-- many tenants takes them in whatever order the write reaches them; shared advisory
-- locks do not exclude each other, so two placements cannot deadlock on the order, and
-- the one thing either can wait behind is a move, which never waits for anything and so
-- cannot close a cycle.
--
-- data: exempt reason: this file writes no rows; it puts the declaration on the write
-- that does, migrations/000045's platformkit_place_tenants.
CREATE FUNCTION platformkit_ledger_placement_lock() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
	-- The tenant's own key, the same expression migrations/000044's claim trigger
	-- performs and the same one kit/events/ledger.go's tenantLockKey asks for.
	PERFORM pg_advisory_xact_lock_shared(hashtextextended('events ledger tenant ' || NEW.id::text, 0));
	RETURN NEW;
END
$$;

-- OF app, so a write that touches the row for some other reason says nothing about
-- its ledger; the WHEN clause then holds the other half, that the app went from
-- nobody's to somebody's. A step the other way is not expressible (000043: a tenant
-- does not move between apps) and this file does not pretend to guard it.
CREATE TRIGGER platformkit_tenants_app_lock
	BEFORE UPDATE OF app ON tenants
	FOR EACH ROW
	WHEN (OLD.app = '' AND NEW.app <> '')
	EXECUTE FUNCTION platformkit_ledger_placement_lock();

COMMENT ON FUNCTION platformkit_ledger_placement_lock() IS
	'platformkit: takes the shared advisory lock of a tenant whose app this transaction is naming, the key '
	'migrations/000044 gives an unscoped claim and kit/events.MoveLedger asks for exclusively, so no placement '
	'can commit a tenant into an app whose ledger move is past its final read of that tenant — the move either '
	'refuses over it or the placement serializes behind it and owes the move its own boot runs next';
