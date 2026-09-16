-- The trail's idempotency key gains the tenant.
--
-- audit_events.event_id was unique on its own, which says "one outbox event is
-- one trail row anywhere in this installation". That is one row too few. The
-- kernel's handled table claims (event_id, durable) per subscription and this
-- index is the second lock behind it — for an operator replaying a row, or a
-- handler that failed after writing — and the thing it locks is a row in one
-- tenant. A global unique makes one tenant's trail row silently suppress
-- another's if two tenants were ever handed the same event id: unlikely with
-- gen_random_uuid(), and "unlikely" is not the guarantee a table like this one
-- is for. A restore, an import or a replay across tenants makes it likely.
--
-- The insert already says ON CONFLICT (event_id) DO NOTHING and now says
-- (tenant_id, event_id), which is the key it always meant.
--
-- The new index is created first, so the table is never without one: dropping
-- the old one first would leave a window in which a redelivery could write a
-- duplicate.
CREATE UNIQUE INDEX audit_events_tenant_event ON audit_events (tenant_id, event_id);
DROP INDEX audit_events_event;
