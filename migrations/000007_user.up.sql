-- Users: one row per person per tenant.
--
-- There is no membership table, and that is a consequence rather than a
-- simplification. One tenant per host means a request is about one tenant
-- before it is about anybody, so the same person working in two tenants is two
-- rows with two sets of roles and two passwords — which is what "tenant
-- isolation belongs to the database" costs, and what makes every query about a
-- user an ordinary tenant-scoped query.
--
-- The entity is modules/user/contracts/user.go.

CREATE TABLE users (
	id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id     uuid NOT NULL,
	created_at    timestamptz NOT NULL DEFAULT now(),
	updated_at    timestamptz NOT NULL DEFAULT now(),
	deleted_at    timestamptz,

	email         text NOT NULL,
	display_name  text NOT NULL DEFAULT '',
	-- invited, active or inactive.
	status        text NOT NULL DEFAULT 'invited',
	-- The roles this user holds. They are names; what a name grants is the auth
	-- module's roles table, so a role can be re-granted without touching a user.
	roles         text[] NOT NULL DEFAULT '{}',
	-- argon2id, in the PHC encoding. NULL for a user who has never set one,
	-- which is every invited user and every user who only signs in through OIDC.
	password_hash text
);

-- Unique per tenant and case-insensitively, because an address that differs
-- only in case is the same mailbox and two rows for it are two accounts nobody
-- meant to have. Partial, so a deleted user releases the address.
CREATE UNIQUE INDEX users_tenant_email ON users (tenant_id, lower(email))
	WHERE deleted_at IS NULL;

-- The default list page, and the login lookup, which is the same index.
CREATE INDEX users_tenant_created ON users (tenant_id, created_at DESC, id)
	WHERE deleted_at IS NULL;

ALTER TABLE users ENABLE ROW LEVEL SECURITY;
ALTER TABLE users FORCE ROW LEVEL SECURITY;

CREATE POLICY users_tenant ON users
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
