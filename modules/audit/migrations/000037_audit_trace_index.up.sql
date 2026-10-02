-- pkit: autocommit=true
-- The index behind "every row this trace touched".
--
-- traceparent is stored verbatim, as W3C Trace Context spells it, and the trace
-- id is its second field rather than a column of its own — one string kept in
-- two columns is two strings that can disagree. That is affordable only because
-- this expression index makes the id searchable, which is what makes the
-- decision to store one string the cheap one rather than the lazy one.
--
-- CONCURRENTLY and alone for the same reason as 000036.
CREATE INDEX CONCURRENTLY IF NOT EXISTS audit_events_tenant_trace
	ON audit_events ((split_part(traceparent, '-', 2))) WHERE traceparent IS NOT NULL;
