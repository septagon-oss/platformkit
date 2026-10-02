-- pkit: autocommit=true
-- The index behind "every row this request wrote".
--
-- The question is asked with an id in hand — a caller quoted one, or a log line
-- named one — and it is asked of one tenant's trail. Partial, because most rows
-- belong to no request at all (a job, a handler reacting to another event, a row
-- from before 000035) and an index over those is a table's worth of NULLs.
--
-- One statement per file, and CONCURRENTLY, because audit_events already exists:
-- the plain build takes a SHARE lock over a table whose writers are every
-- request in the installation. See migrations/README.md's autocommit row.
CREATE INDEX CONCURRENTLY IF NOT EXISTS audit_events_tenant_request
	ON audit_events (tenant_id, request_id) WHERE request_id IS NOT NULL;
