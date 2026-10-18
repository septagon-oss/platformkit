-- Bearer tokens for a person's own integrations.
--
-- A session cookie proves a browser. Nothing here proves a script: until this
-- file the only credential this platform accepted was a cookie, so a mobile
-- shell or a deploy bot had to hold a person's password and impersonate their
-- browser, which is the arrangement every incident report eventually names.
--
-- One table, and the shape is sessions.id_hash's: the row is keyed by
-- sha256(token), the token itself is written nowhere, so a dump is a list of
-- hashes rather than a bag of live credentials. SHA-256 rather than argon2id for
-- the reason that file gives — the input is 256 bits from crypto/rand, so there
-- is no dictionary to run and nothing for a slow hash to buy.
--
-- No updated_at: the only mutable columns are the two timestamps a sweep and a
-- throttle write, and a column that claims to track "the row was touched" while
-- nothing reads it is a column that lies (rule 7). last_used_at is read — by the
-- list a person looks at to decide which key they stopped using a year ago — and
-- it never extends expires_at, because a bearer that renews itself on use is the
-- thing SessionMaxLifetime's comment refuses for a session.
CREATE TABLE api_tokens (
	id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id uuid NOT NULL,
	-- Who this key acts as. A token is a credential for a person, never a
	-- person: a machine integration that needs its own identity is a user row,
	-- which is what this module already says about service accounts.
	user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	-- Who minted it. No ON DELETE CASCADE: deleting the person who made a key
	-- must not silently delete a key another integration depends on. The sweep
	-- and the erasure path revoke it instead, and an incident review asks who
	-- made the key that outlived them.
	created_by uuid NOT NULL,
	name text NOT NULL,
	-- Permission keys, checked at issue against the catalogue of what any module
	-- declares and against what the holder's own roles already grant. A token can
	-- therefore never hold authority its holder does not have — a grant that
	-- cannot be exercised reads, to whoever wrote it, as one that can.
	scopes text[] NOT NULL DEFAULT '{}',
	token_hash bytea NOT NULL UNIQUE,
	created_at timestamptz NOT NULL DEFAULT now(),
	expires_at timestamptz NOT NULL,
	last_used_at timestamptz NOT NULL DEFAULT now(),
	revoked_at timestamptz,
	CONSTRAINT api_tokens_expiry_after_issue CHECK (expires_at > created_at),
	CONSTRAINT api_tokens_name_not_empty CHECK (name <> ''),
	CONSTRAINT api_tokens_has_a_scope CHECK (cardinality(scopes) > 0),
	CONSTRAINT api_tokens_revoked_after_issue CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);

-- The list a person reads is by holder; the sweep walks expiry.
CREATE INDEX api_tokens_user ON api_tokens (tenant_id, user_id);
CREATE INDEX api_tokens_expires_at ON api_tokens (expires_at);

ALTER TABLE api_tokens ENABLE ROW LEVEL SECURITY;
ALTER TABLE api_tokens FORCE ROW LEVEL SECURITY;
CREATE POLICY api_tokens_tenant ON api_tokens
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
