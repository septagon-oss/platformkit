-- Tenants and the hosts they are served at: the control plane.
--
-- These two tables are the exception to idea 4, and the exception is what makes
-- the rule work. Every other table answers "which tenant owns this row?" with a
-- tenant_id column and a policy that matches it against the transaction's
-- setting. These tables answer the question before there is a transaction to
-- ask it in: a request arrives at a host, and something has to say which tenant
-- that is before kit/db can scope anything.
--
-- So they are declared exempt, in the comment the convention uses, and they are
-- still not unprotected. The policy lets a system transaction — the host
-- resolution kit/httpx opens, the control-plane routes of modules/tenant — see
-- everything, and lets an ordinary tenant transaction see exactly one row: its
-- own. A tenant reading `SELECT * FROM tenants` under its own transaction gets
-- itself and no one else, which is the same guarantee every other table gives,
-- reached by naming the row instead of a column on it.

CREATE TABLE tenants (
	id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	slug       text NOT NULL,
	name       text NOT NULL,
	-- active or suspended. A suspended tenant resolves to nothing, so its hosts
	-- read as "no site here" rather than as a locked door; see modules/tenant.
	status     text NOT NULL DEFAULT 'active',
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	deleted_at timestamptz
);

-- Partial, so a deleted tenant releases its slug and a new one may take it.
CREATE UNIQUE INDEX tenants_slug ON tenants (slug) WHERE deleted_at IS NULL;

CREATE TABLE tenant_hosts (
	-- The host is the key: one host is served by one tenant, and that is the
	-- whole of routing. A tenant may hold many.
	host       text PRIMARY KEY,
	tenant_id  uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
	created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX tenant_hosts_tenant ON tenant_hosts (tenant_id);

ALTER TABLE tenants ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenants FORCE ROW LEVEL SECURITY;
ALTER TABLE tenant_hosts ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_hosts FORCE ROW LEVEL SECURITY;

-- WITH CHECK is system-only on both: a tenant transaction reads its own row and
-- writes nothing here, so renaming yourself is a control-plane operation like
-- any other and goes through the routes that hold the capability.
CREATE POLICY tenants_scope ON tenants
	USING (platformkit_is_system() OR id = platformkit_current_tenant_id())
	WITH CHECK (platformkit_is_system());

CREATE POLICY tenant_hosts_scope ON tenant_hosts
	USING (platformkit_is_system() OR tenant_id = platformkit_current_tenant_id())
	WITH CHECK (platformkit_is_system());

COMMENT ON TABLE tenants IS 'platformkit:tenant-scoping-exempt: control plane, read by the loader under system access';
COMMENT ON TABLE tenant_hosts IS 'platformkit:tenant-scoping-exempt: control plane, read by the loader under system access';
