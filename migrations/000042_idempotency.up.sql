-- The idempotency claims: one row per (tenant, caller, route, key), holding the
-- answer the first request under that key finally got.
--
-- A command whose response was lost in the network is a command the client
-- cannot retry: it does not know whether the write happened, and the four client
-- scripts this table exists for each answered that with a stored body, an
-- operator's button and a sentence promising nothing was sent automatically.
-- The answer is one row: the same key and the same bytes get the stored response
-- back, a different body under the same key is a different command and is
-- refused, and a repeat while the first is still running is refused too. kit/
-- httpx/idempotency.go is the only door.
--
-- The row carries its tenant and its caller because both are primary-key
-- columns and one key must never answer two customers or two accounts. They are
-- not a policy: the rows are written outside any tenant's transaction, by the
-- request that claims them, ahead of the command they describe — a claim written
-- inside the request's own transaction is invisible to the concurrent repeat it
-- exists to refuse. Same argument, same shape as platformkit_limits.
--
-- Two clocks, both Postgres's: claimed_at bounds the in-flight marker at five
-- minutes so a process that died between the claim and the answer cannot hold a
-- key hostage for a day, and expires_at bounds the stored response at twenty-four
-- hours after it was claimed. A third, owner_lease, says which of those two
-- readings the row is allowed to have: while a request owns the claim it renews
-- the lease, so a command slower than the marker it wrote is a command the purge
-- must pass over, and a lease nobody renewed any longer is a command whose process
-- is gone. NULL is the same statement as an expired one — no owner is attached —
-- and it is what the record step writes on its way out, because an answered row is
-- not being answered any longer. The stored body can carry personal data, which is
-- the second reason the day is a day and not indefinite. The table is in no export
-- for the first reason: it is the transport's own memo, and walking it would hand
-- one person a body somebody else submitted.

CREATE TABLE platformkit_idempotency (
	tenant_id    uuid        NOT NULL,
	actor_id     uuid        NOT NULL,
	-- "POST /tasks/items/{id}/records": the mounted pattern, never the request's
	-- own path, which is the difference between a bounded set of keys and one a
	-- caller can inflate. The concrete path and query are inside request_hash.
	operation    text        NOT NULL,
	key          text        NOT NULL,
	request_hash bytea       NOT NULL,
	claimed_at   timestamptz NOT NULL,
	-- An unsettled row is a command in flight; a settled one is an answer.
	settled      boolean     NOT NULL DEFAULT false,
	status       integer     NOT NULL DEFAULT 0,
	content_type text        NOT NULL DEFAULT '',
	-- The three headers a client acts on — HX-Redirect, Location, Retry-After.
	-- Content-Type is the column above it and is not stored twice.
	headers      jsonb       NOT NULL DEFAULT '{}',
	-- NULL on a settled row means it ran and its answer was too big to hold, or
	-- had already reached the wire. That is an answer of its own: a repeat is
	-- told to read the result rather than send the write again.
	response     bytea,
	-- The first request's own id, so a replayed answer names the request that
	-- produced it and an operator can be quoted something findable.
	request_id   text        NOT NULL DEFAULT '',
	expires_at   timestamptz NOT NULL,
	-- The owner's own renewal. Fresh means a request is answering this claim right
	-- now, whatever its deadlines say; expired or NULL means nobody is, which is the
	-- only state the purge may delete an unsettled row in. See kit/httpx/idempotency.go.
	owner_lease  timestamptz,
	PRIMARY KEY (tenant_id, actor_id, operation, key)
);

-- The purge's query: the answers a day old.
CREATE INDEX platformkit_idempotency_expires_at ON platformkit_idempotency (expires_at);

ALTER TABLE platformkit_idempotency ENABLE ROW LEVEL SECURITY;
ALTER TABLE platformkit_idempotency FORCE ROW LEVEL SECURITY;

-- System only, in both directions, as platformkit_limits is: the tenant column is
-- real and NOT NULL, so a cross-tenant replay is impossible by construction, but no
-- tenant transaction ever reads or writes this table and a policy a tenant could
-- match would be a policy nothing consults.
CREATE POLICY platformkit_idempotency_scope ON platformkit_idempotency
	USING (platformkit_is_system())
	WITH CHECK (platformkit_is_system());

COMMENT ON TABLE platformkit_idempotency IS 'platformkit:tenant-scoping-exempt: control plane, claimed outside any tenant transaction so a concurrent repeat can see it; every row names its tenant and its caller and is reachable only through kit/httpx, which qualifies by both';
