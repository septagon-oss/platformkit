-- Narrowing it back can fail, and should: a database holding two tenants' rows
-- for one event id is one the old index cannot describe.
CREATE UNIQUE INDEX audit_events_event ON audit_events (event_id);
DROP INDEX audit_events_tenant_event;
