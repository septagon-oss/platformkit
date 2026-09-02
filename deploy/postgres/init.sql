-- The application connects as platformkit_app, never as the owner. The role is
-- NOSUPERUSER NOBYPASSRLS, so the FORCE ROW LEVEL SECURITY policies installed by
-- the tenancy migration (E1) actually constrain it; a superuser would silently
-- bypass every policy and the isolation tests would pass while proving nothing.
-- Migrations connect as the owner (postgres), which holds the DDL rights.
CREATE ROLE platformkit_app LOGIN PASSWORD 'platformkit' NOSUPERUSER NOBYPASSRLS;

GRANT USAGE ON SCHEMA public TO platformkit_app;

-- Tables and sequences do not exist yet: migrations create them later, as the
-- owner. Default privileges hand each new one to the app role as it appears.
ALTER DEFAULT PRIVILEGES IN SCHEMA public
	GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO platformkit_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
	GRANT USAGE, SELECT ON SEQUENCES TO platformkit_app;
