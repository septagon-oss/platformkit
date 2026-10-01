-- Provenance for records written through an application's declared seed writers.
-- The key may be a natural owner key or an opaque name for a keyless resource.
CREATE TABLE seed_keys (
	tenant_id uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
	module text NOT NULL,
	entity text NOT NULL,
	key text NOT NULL,
	kind text NOT NULL CHECK (kind IN ('starter', 'demo')),
	record_id uuid,
	PRIMARY KEY (tenant_id, module, entity, key)
);

CREATE INDEX seed_keys_tenant_resource_kind ON seed_keys (tenant_id, module, entity, kind);

ALTER TABLE seed_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE seed_keys FORCE ROW LEVEL SECURITY;

CREATE POLICY seed_keys_tenant ON seed_keys
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
