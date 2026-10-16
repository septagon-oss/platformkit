-- pkit: autocommit=true
-- The exact occurrence filter must remain available while the trail accepts writes.
CREATE INDEX CONCURRENTLY IF NOT EXISTS audit_events_operation ON audit_events (tenant_id, operation_ref)
 WHERE operation_ref IS NOT NULL;
