-- Sessions and roles: who is signed in, and what a role name grants.
--
-- Both are ordinary tenant-owned tables with the shape migrations/000001
-- describes, and that is the point of the design rather than an accident of it.
-- A session is looked up inside the transaction of the tenant the request host
-- resolved to, so a session created on one tenant's host and presented on
-- another's is a row the policy does not return: the caller is anonymous, and
-- no Go code had to compare two tenant ids to make it so.

CREATE TABLE sessions (
	-- The id is the credential: it is what the platformkit_session cookie
	-- carries. 122 bits of randomness, over TLS, in an HttpOnly cookie, scoped
	-- by row-level security to the tenant that issued it.
	id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id    uuid NOT NULL,
	user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	created_at   timestamptz NOT NULL DEFAULT now(),
	expires_at   timestamptz NOT NULL,
	-- Touched at most once every few minutes, which is what makes the expiry
	-- slide without a write per request.
	last_seen_at timestamptz NOT NULL DEFAULT now(),
	user_agent   text NOT NULL DEFAULT '',
	ip           text NOT NULL DEFAULT ''
);

-- "Sign me out everywhere", and the sweep that will retire expired rows.
CREATE INDEX sessions_tenant_user ON sessions (tenant_id, user_id);
CREATE INDEX sessions_expires_at ON sessions (expires_at);

CREATE TABLE roles (
	tenant_id   uuid NOT NULL,
	name        text NOT NULL,
	-- The permissions the role grants, as "<resource>:<action>" tokens. The
	-- single element '*' grants every permission there is.
	permissions text[] NOT NULL DEFAULT '{}',
	created_at  timestamptz NOT NULL DEFAULT now(),
	updated_at  timestamptz NOT NULL DEFAULT now(),
	PRIMARY KEY (tenant_id, name)
);

ALTER TABLE sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE sessions FORCE ROW LEVEL SECURITY;
ALTER TABLE roles ENABLE ROW LEVEL SECURITY;
ALTER TABLE roles FORCE ROW LEVEL SECURITY;

CREATE POLICY sessions_tenant ON sessions
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));

CREATE POLICY roles_tenant ON roles
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
