-- The handled ledger and the dead letters move with the durables that renamed them.
--
-- A durable is half of the primary key of both ledgers (000003_handled, 000005_dead_letters),
-- and decision 0074 rule 7 put the app into it: what was `billing-plan-created` is
-- `acme+billing-plan-created` from `appname.Durable`. Nothing else in the row changes, so
-- every row written before an installation names an app now carries a key its own
-- subscription will never ask for again.
--
-- Two consequences, both of which this file removes:
--
--   - `DeliverAll` redelivers everything the transport still holds, and a claim is a
--     row keyed by (event_id, durable). Left alone, every event a subscription handled
--     under the unscoped name is first-time work under the scoped one — the handler runs
--     twice for work that already ran. The handled ledger is the whole of at-least-once
--     delivery's "at most once" half; a rename that leaves it behind throws that half
--     away for one release.
--   - A targeted replay deletes the ledger rows of one durable by exact name
--     (kit/events/replay.go). Under the new name it matches nothing, so the replay runs
--     nothing, and the same durable's dead letters stay where no operator replaying that
--     consumer can see them.
--
-- The predicate is `position('+' in durable) = 0`, and that is exact rather than
-- heuristic: '+' is in no app slug, no module name and no event name (kit/appname's
-- Durable says why the join character is a plus), so an unscoped durable can only be a
-- durable written before the app segment existed. Each row moves to the app of the
-- tenant that owns it — 000030 made that column non-null for every row in the table —
-- which is why one pass over the ledger serves every app of a database that hosts many:
-- the rename is per tenant's app, not per boot.
--
-- An app that names no slug gets nothing moved, because its durable did not change:
-- `appname.Durable` with no slug set answers the unscoped name, so the rows already
-- carry the key its subscription will ask for. `app <> ''` is that case, not a filter
-- over rows somebody should look at.
--
-- The rolling window — an old pod and a new pod delivering one consumer at once — is a
-- collision, and a collision is an answer rather than an error. One event handled twice
-- through the window, once under each spelling, is two rows saying the same thing: this
-- event ran for this consumer. The scoped row is kept and the unscoped twin deleted, so
-- the UPDATE below cannot fail on the key it is writing.

-- Both statements below read and write across every tenant, and both would write nothing
-- if they did not: the runner sets `platformkit.system_access` on a RunSystem transaction
-- and on a `phase=data` drain and around a schema file never, so the policy 000001 puts
-- over `tenants` and the two ledgers' own `platformkit_tenant_match` answer this file's
-- statements with an empty set at any migrate role that owns these tables without being a
-- superuser — the statement is accepted, the version is recorded, and nothing moved. That
-- is the trap migrations/README.md, "A file that writes rows" names, and the reason
-- 000029 declines to write at all.
--
-- This file cannot decline and cannot be a data half: a drain windows over the table's
-- primary key, and both ledgers are keyed by (event_id, durable) — a composite — which
-- kit/db/backfill.go's `primaryKey` refuses by name. So the file sets the marker itself,
-- the way a drain does. That is a statement of intent and not a grant of privilege:
-- kit/db/tx.go records that these GUCs are placeholders, which PostgreSQL makes USERSET,
-- so any role that can open a connection could already set the marker; the value here is
-- that the file says in the ledger, where a reader of it can see, that its writes are the
-- control plane's and not one tenant's. `is_local` is true, so the marker lives exactly
-- for this file's transaction and no longer.
SELECT set_config('platformkit.system_access', 'true', true);

-- The collision first, in both ledgers, then the move. Swapping the two orders would make
-- the UPDATE raise a duplicate key on the very event the window duplicated.
DELETE FROM platformkit_handled AS h
	USING tenants AS t
	WHERE t.id = h.tenant_id AND t.app <> ''
		AND position('+' IN h.durable) = 0
		AND EXISTS (SELECT 1 FROM platformkit_handled AS kept
			WHERE kept.event_id = h.event_id AND kept.durable = t.app || '+' || h.durable);

UPDATE platformkit_handled AS h
SET durable = t.app || '+' || h.durable
	FROM tenants AS t
	WHERE t.id = h.tenant_id AND t.app <> ''
		AND position('+' IN h.durable) = 0;

DELETE FROM platformkit_dead_letters AS d
	USING tenants AS t
	WHERE t.id = d.tenant_id AND t.app <> ''
		AND position('+' IN d.durable) = 0
		AND EXISTS (SELECT 1 FROM platformkit_dead_letters AS kept
			WHERE kept.event_id = d.event_id AND kept.durable = t.app || '+' || d.durable);

UPDATE platformkit_dead_letters AS d
SET durable = t.app || '+' || d.durable
	FROM tenants AS t
	WHERE t.id = d.tenant_id AND t.app <> ''
		AND position('+' IN d.durable) = 0;

COMMENT ON COLUMN platformkit_handled.durable IS 'The subscription that did the work, formed by kit/appname.Durable: <app>+<module>+<event>, or the unscoped <module>+<event> while the installation names no slug. 000031 moved rows written before the app segment existed.';
COMMENT ON COLUMN platformkit_dead_letters.durable IS 'The subscription that gave up, spelled the way kit/appname.Durable spells it now and moved to that spelling by 000031. A targeted replay deletes by this name, so a row that never moved would be invisible to the one command that reads it.';
