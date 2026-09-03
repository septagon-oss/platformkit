-- The rate limit counters: one row per key per window, shared by every replica.
--
-- A counter in a process's memory is three limits when three pods are running
-- and none after a deploy, which is what modules/auth's lockout said about
-- itself until this table existed. kit/limit writes it with one statement —
-- INSERT ... ON CONFLICT DO UPDATE, so two replicas raising the same count are
-- serialized by the row's own lock rather than by reading a number and writing
-- it back.
--
-- The key carries its tenant. It is the tenant's id, a slash, and whatever the
-- caller counts by (kit/limit's scoped), so a limit is per customer without a
-- column and without a policy that reads one — which matters because the rows
-- are written outside any tenant's transaction: a failed login rolls its own
-- transaction back, and a count that rolled back with it would be a limiter
-- that never counts the attempts it exists to count.

CREATE TABLE platformkit_limits (
	key          text PRIMARY KEY,
	-- When the open window started. A window is closed when this is older than
	-- the caller's window, and the same row is then started again.
	window_start timestamptz NOT NULL DEFAULT now(),
	count        integer NOT NULL DEFAULT 0
);

-- The purge's query: the rows whose window closed long ago.
CREATE INDEX platformkit_limits_window_start ON platformkit_limits (window_start);

ALTER TABLE platformkit_limits ENABLE ROW LEVEL SECURITY;
ALTER TABLE platformkit_limits FORCE ROW LEVEL SECURITY;

-- System only, in both directions. A tenant transaction has no business reading
-- another tenant's counters and none writing its own: the door is kit/limit,
-- which holds the capability, and there is no other.
CREATE POLICY platformkit_limits_scope ON platformkit_limits
	USING (platformkit_is_system())
	WITH CHECK (platformkit_is_system());

COMMENT ON TABLE platformkit_limits IS 'platformkit:tenant-scoping-exempt: control plane, counted outside any tenant transaction; the tenant is the first field of the key';
