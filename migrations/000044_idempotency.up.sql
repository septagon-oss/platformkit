-- The idempotency records: one row per keyed write, held for a day, shared by
-- every replica.
--
-- A caller's `Idempotency-Key` is a promise that a retried write is one write, and
-- a promise held in one process's memory is a promise three pods cannot keep and no
-- deploy keeps at all. So the record is a row, in the shape migrations/000021_limits
-- sets for the same problem one package over: written outside any tenant's
-- transaction by a package that holds the capability, expired by a scheduled purge,
-- and read by nobody else.
--
-- The identity is five columns and not one key. kit/limit gets the same property
-- from putting a fixed-length tenant id at the front of every key string; taking it
-- with columns instead means no caller can put a separator in the one field it
-- controls — the key — and be read as another tenant's or another person's record.
-- uuid.Nil is a value both identifier columns can take: for `tenant` it is the host
-- that resolved nobody, the same bucket kit/limit counts an unresolved address in,
-- and for `caller` it is the anonymous public surface, which has no account to name.

CREATE TABLE platformkit_idempotency (
	tenant       uuid        NOT NULL,
	caller       uuid        NOT NULL,
	method       text        NOT NULL,
	route        text        NOT NULL,
	-- The caller's own string. Bounded above by the middleware — a key is a
	-- primary-key column, so an unbounded value is an unbounded row anybody can
	-- mint — and never normalised: the same bytes or a different record is what
	-- makes a conflicting body decidable.
	key          text        NOT NULL,
	-- What the request was: the method, route, query, content type and body
	-- digested together. A replay is the same write, and this is the only way to
	-- tell that from a different one that happens to reuse the key.
	fingerprint  bytea       NOT NULL,
	-- Which attempt owns the record. A claim is taken by one statement and
	-- completed by another, and a completion that names a claim the row no longer
	-- holds writes nothing: two retries of one dropped response must not both
	-- believe they are the write.
	claim        uuid        NOT NULL,
	claimed_at   timestamptz NOT NULL DEFAULT now(),
	-- Whether this attempt was let in to do the write. It is the one fact that
	-- separates the two ways a claim can outlive its lease: an attempt that died
	-- before anybody acted on it, which the next attempt may take up, and an attempt
	-- that may have committed its write and never got its answer down — the store
	-- went away mid-request, or the process with it. The second is answered as spent
	-- and never re-run, because re-running it is the duplicate the key exists to
	-- prevent and no length of lease shortens that window: the window is the write.
	-- A boolean and not a timestamp — nothing reads the moment, only the verdict.
	began        boolean     NOT NULL DEFAULT false,
	-- NULL while the write is in flight. The takeover of an abandoned claim and the
	-- purge both read this column rather than a status field, because "has an
	-- answer" is the fact and a status enum is a second answer to the same question.
	completed_at timestamptz,
	-- Whether an answer is held. False is the spent record: the write completed and
	-- its answer could not be kept, which is a 409 forever and never a re-run.
	recorded     boolean     NOT NULL DEFAULT false,
	status       integer,
	content_type text,
	body         bytea,
	-- The answer's own response headers, JSON in a bytea the way the body is.
	-- A 303 whose Location is gone is not the same answer: it is a status with
	-- nothing to act on, and a caller who retried a form that worked could not reach
	-- the page it said the write had finished at. Which names are worth keeping is
	-- kit/httpx's decision (idempotency.go's keptHeaders); this row holds verbatim
	-- what it is handed, the same way it holds the body.
	headers      bytea,
	PRIMARY KEY (tenant, caller, method, route, key)
);

-- The purge's query: the records a day past their claim. Without this index the
-- scheduled sweep is a full scan of a table whose whole purpose is to be written on
-- the path of a request.
CREATE INDEX platformkit_idempotency_claimed_at ON platformkit_idempotency (claimed_at);

ALTER TABLE platformkit_idempotency ENABLE ROW LEVEL SECURITY;
ALTER TABLE platformkit_idempotency FORCE ROW LEVEL SECURITY;

-- System only, in both directions, exactly as platformkit_limits is. A tenant
-- transaction has no business reading another tenant's records and no business
-- writing its own: the door is kit/idempotency, which holds the capability, and
-- there is no other. Deliberately no foreign key to the tenant either — the record
-- is written in a transaction that resolves no tenant, and a claim refused because
-- somebody is deleting a tenant row would be a write refused for a reason with no
-- sentence to explain it; such a row is unreachable and the purge deletes it.
CREATE POLICY platformkit_idempotency_scope ON platformkit_idempotency
	USING (platformkit_is_system())
	WITH CHECK (platformkit_is_system());

COMMENT ON TABLE platformkit_idempotency IS 'platformkit:tenant-scoping-exempt: control plane, written outside any tenant transaction; the tenant is a column of the key and uuid.Nil is the host that resolved none';
