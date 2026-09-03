DROP INDEX IF EXISTS tenant_hosts_primary;
ALTER TABLE tenant_hosts DROP COLUMN IF EXISTS is_primary;
