-- Dropping the control plane leaves an application that resolves no host.
DROP TABLE IF EXISTS tenant_hosts;
DROP TABLE IF EXISTS tenants;
