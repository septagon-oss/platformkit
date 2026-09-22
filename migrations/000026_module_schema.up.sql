-- One schema per module, opened to the role that will read it.
--
-- A module's objects live in a schema named exactly the module's migration
-- owner, and the kernel's own tables stay in public. Creating that schema is
-- the easy half. What makes a module readable is USAGE on the schema plus the
-- table and sequence privileges every later object hands to the application
-- role, and today only the deployment writes them: apps/platformkit/
-- postgres-init.sql grants them for public and names the role in the same
-- sentence. A module's migration may not do that — it does not know, and must
-- not know, which role its deployment runs beside. So this function discovers
-- the grantees, and what to hand each one, instead of naming either, and it is
-- the one line a module's first schema revision runs.
--
-- Why it names no role and no privilege. The runner already refuses to name a
-- role: when it revokes application access to the ledger it reads the grantees
-- out of the object's own ACL (kit/db/migrate.go, migrationLedger). The same
-- discovery happens here, one catalog earlier, with one condition more: the
-- rows it reads are the ones the current role granted for the namespace the
-- migration runs in — pg_default_acl rows whose defaclrole is
-- current_user::regrole, whose defaclnamespace is current_schema() and whose
-- defaclobjtype is 'r' for tables or 'S' for sequences, grantees decoded from
-- defaclacl with aclexplode. Each grantee such a row names is handed USAGE on
-- the new schema and then the privileges its own row names, for its own object
-- kind and no other: grantee by grantee and privilege by privilege, never a
-- union of several rows and never SELECT, INSERT, UPDATE, DELETE by fiat. A
-- grantee no such row names is handed nothing, here or anywhere else.
--
-- Reading the privilege list out of that row rather than writing one here is
-- what keeps the deployment in charge of its own roles. Row-level security
-- binds which rows a grantee may see; it says nothing about whether it may
-- write one, and this grant is the deployment's, not the module's. A role the
-- deployment pinned to SELECT by default reads module tables and does not write
-- them; a fixed SELECT, INSERT, UPDATE, DELETE list would make this kernel
-- function the thing that turns a deployment's reporting role into a writer of
-- every module table, in every schema the deployment ever opened. The second
-- review's TestModuleSchemaGrantsNoMoreThanTheDeploymentAlreadyHandedOut runs
-- one INSERT on both sides of that line — the deployment's own namespace, and a
-- schema this function opened — and refuses a function that blurs it.
--
-- Which namespace, and why this one and no other. PostgreSQL scopes a default
-- privilege pinned IN SCHEMA to that namespace and to no other: a deployment
-- that pins SELECT for a role in one namespace and INSERT for it in a second
-- leaves that role able to read the first and write the second, and gives it
-- neither privilege in a third namespace (measured both ways: a database-wide
-- row and a namespace row do merge inside that namespace, and two namespace rows
-- never merge across each other). Reading every row the migration role owns for
-- an object kind, whatever namespace it is pinned to, and handing out the union
-- would therefore write into a module's schema a privilege the deployment handed
-- to that role nowhere — which is what the third review's
-- TestModuleSchemaGrantsNoPrivilegeTheDeploymentPinnedElsewhere pins. So the
-- filter is current_schema(): the schema this function creates lands beside the
-- namespace the caller's search_path resolves to, and that namespace's rows are
-- the deployment's own answer for objects created where this one is being
-- created. In the reference deployment that namespace is public, which is where
-- apps/platformkit/postgres-init.sql pins its pair; under dbtest.URLs it is the
-- test's own schema, which is where that helper pins its pair; a session that
-- names no path at all resolves to public, because an unset search_path is
-- "$user", public and the schema named by the role does not exist. One rule,
-- three answers, and in each the rows are the deployment's, written by the
-- deployment, read unchanged.
--
-- Database-wide defaults — defaclnamespace = 0, an ALTER DEFAULT PRIVILEGES with
-- no IN SCHEMA — are not mirrored, and mirroring them would change nothing about
-- what a module's tables hand out: such a row already applies to every object
-- the role creates in every schema, a schema created after the pin included
-- (measured: a role pinned database-wide holds SELECT on a table created in a
-- schema that did not exist when the pin was written, with no statement written
-- for it). What such a row does not do is open a namespace, because schema
-- USAGE is not a privilege on tables or sequences and the only thing that hands
-- USAGE here is the GRANT this function runs for a grantee it found in the
-- namespace. A deployment whose defaults are pinned database-wide and nowhere
-- else therefore finds its module schemas opened to their owner alone — the same
-- consequence as pinning none, and stated beside the function's name in
-- migrations/README.md. What it costs such a deployment is one loud error at the
-- first read of a module's table, "permission denied for schema", which names
-- the grant that is missing instead of quietly leaving one the deployment never
-- wrote. A session whose search_path resolves to no schema at all cannot reach
-- this function through the runner: the ledger's own CREATE TABLE already has to
-- land somewhere.
--
-- The grantees are decoded exactly as the ledger block decodes them, including
-- grantee 0 spelled PUBLIC: 0::oid::regrole renders as "-" and
-- pg_get_userbyid(0) as "unknown (OID=0)", and either would reach EXECUTE as a
-- syntax error. The reference deployment has no PUBLIC default-privilege row,
-- so it never takes that branch; a deployment that grants its defaults to
-- PUBLIC in the namespace a module's schema opens beside does, and getting it
-- wrong there would be a migration that fails on the customer's own
-- configuration rather than on ours. Such a deployment finds
-- every role in the cluster able to open its module schemas, because that is
-- the default it pinned; migrations/README.md says so beside the name, and
-- TestModuleSchemaDiscoversPublicAsAGrantee keeps the decode honest.
--
-- What it refuses, and what the refusal says. Each message names the rule it
-- applied, because the caller is a migration file and has nothing else to go
-- on but the exception text.
--
--   * an owner the ledger's own grammar refuses — kit/db/migration_files.go,
--     migrationOwner, `^[a-z][a-z0-9_-]*$`, NULL included — because a schema
--     the ledger cannot name is a schema nothing can attribute: "is not a
--     module schema name; an owner must match ^[a-z][a-z0-9_-]*$".
--   * an owner longer than 63 bytes, because PostgreSQL shortens an identifier
--     silently rather than refusing it. The ledger would keep the name the
--     module wrote while pg_namespace held a different one, and the
--     tenant-scope walk, which matches a schema against the ledger's owners,
--     would then check nothing at all and report success: "silently truncated".
--   * public, information_schema and any pg_ prefix, which belong to the
--     deployment and to the catalog and to no module: "is a namespace the
--     deployment or the catalog owns".
--   * a schema that already exists under another owner. GRANT USAGE and ALTER
--     DEFAULT PRIVILEGES IN SCHEMA both need ownership or CREATE on the
--     schema; half-granting into somebody else's namespace is the failure this
--     function exists to prevent, so it raises loudly instead.
--
-- A second call for a schema this role owns changes nothing and raises
-- nothing: CREATE SCHEMA IF NOT EXISTS, and the schema grant and every
-- default-privilege statement are idempotent on their own. Nothing is ever
-- revoked, and nothing is ever dropped — disabling a module leaves its schema
-- and its data exactly as ADR 0011 requires of an omitted owner.
--
-- It sets no search_path, and the reason is not the one 000001's helpers give.
-- They skip the pin to stay inlinable, which is a reason about how often they
-- run. This one skips it because there is nothing to resolve: it reads only
-- PostgreSQL's own catalogs — which are always on the path regardless — and
-- EXECUTEs statements whose every name is built with %I, so no identifier it
-- uses could arrive through the path, and no table it touches could be
-- shadowed by a temporary one. A SET clause would therefore buy nothing and
-- cost two things: the caller's own path would stop applying inside the
-- function, which is where a future edit that does resolve a name would break;
-- and current_schema() would stop naming the namespace the caller runs in, which
-- is the namespace whose default privileges this function is obliged to read, so
-- a pinned path would leave the grantee loop with no rows and every module
-- schema opened to its owner alone.
--
-- A module's first schema revision is this one line, and its tables come after
-- it, in the module's own later revisions:
--
--   -- modules/cart/migrations/000001_schema.up.sql
--   SELECT platformkit_module_schema('cart');
--
--   -- modules/cart/migrations/000002_carts.up.sql
--   CREATE TABLE cart.carts (tenant_id uuid NOT NULL, id uuid NOT NULL, ...);
--   ALTER TABLE cart.carts ENABLE ROW LEVEL SECURITY;
--   ALTER TABLE cart.carts FORCE ROW LEVEL SECURITY;
--   CREATE POLICY carts_tenant ON cart.carts
--     USING (platformkit_tenant_match(tenant_id))
--     WITH CHECK (platformkit_tenant_match(tenant_id));
--
-- The module names its own schema and public functions and nothing else, which
-- is why the call is unqualified: this function lives where the kernel's SQL
-- lives, and a module reaches it the way it reaches platformkit_tenant_match.
-- There is no grant anywhere in that SQL, and there must never be one.
--
-- The limit, stated rather than hidden: a deployment whose migration role holds
-- no default privileges pinned for the namespace the migration runs in — none at
-- all, or only database-wide ones, which are not this namespace's rows — yields
-- zero grantees, and the schema this function creates is then opened to its owner
-- alone: nspacl stays NULL, and has_schema_privilege for the application role
-- answers false. That is not a failure of the function — the deployment asked for
-- schemas nobody else may open — but it is indistinguishable from a broken
-- installation from the outside, so migrations/README.md says it beside the
-- function's name, and
-- kit/db/dbtest remains the place that performs these same three kinds of
-- statement for a test schema by naming the role it discovered from the test's
-- own URL.

-- This file is 000026 and not 000022. The kernel's own highest revision is
-- 000021; the four numbers above it belong to the modules that took them when
-- they took their own SQL, and readMigrations refuses a source that still
-- lists a version another source adopts (kit/db/migration_files.go, ADR 0011's
-- amendment: one owner per version). The gap above 000021 is those modules,
-- not an accident, and 26 is the next number the kernel may own.

-- The body is the header above executed. Every check that can be made without
-- touching anything is made first, and in that order, so a refusal never leaves
-- a half-open schema: the test that refuses an owner counts, after each refusal,
-- how many schemas of that name exist and expects the number that were already
-- there. The one check that cannot precede the write is the ownership check
-- below — whether a schema belongs to this role is only knowable once it exists,
-- and CREATE SCHEMA IF NOT EXISTS does not report which of its two outcomes it
-- just took.
--
-- Nothing is caught here, and that is deliberate. Two sessions opening the same
-- owner at once reach the same CREATE SCHEMA IF NOT EXISTS, and the catalog
-- settles them on pg_namespace_nspname_index rather than letting both create.
-- Measured with two sessions against this file, the first holding an uncommitted
-- create: when the first commits, the other is refused with 23505
-- unique_violation, its CONTEXT naming this function, and that session's own
-- transaction rolls back with nothing written; when the first rolls back, the
-- other is unblocked and creates the schema inside its own transaction, which
-- commits a schema it made itself. It is not 42710 duplicate_object, and it is
-- not the 42P07 duplicate_table a handler would have named — so the EXCEPTION
-- block that looks like it makes the race safe would not catch the error the
-- race actually raises, and would let the refused session go on believing it had
-- created the schema while the winner's grants are the ones that stand. Two
-- runners of one composition never reach the window at all:
-- kit/db/migrate.go holds pg_advisory_lock(7240101) across the whole
-- composition, and applyMigration runs one file's statements and its ledger
-- INSERT in one transaction, so inside that lock the session that commits a
-- schema commits the history row naming it, and the session that creates nothing
-- commits nothing.
--
-- What that leaves, stated rather than left to be inferred: the way to a schema
-- the ledger does not name is to create one outside the runner — by hand, in a
-- session that never INSERTed a history row. That state is lawful: an empty
-- schema owned by the migration role grants nothing, revokes nothing and is
-- outside the tenant-scope walk, which follows the ledger's owners, and the next
-- revision that names that owner finds the schema there and records the row. It
-- is not an outcome of the race above. In the branch where the committing
-- session created nothing, the catalog refused it with 23505 and its transaction
-- rolled back; in the branch where a schema commits, the session that committed
-- it is the one that created it, and running through the runner it wrote its own
-- history row in the same transaction.
CREATE OR REPLACE FUNCTION platformkit_module_schema(owner text) RETURNS void
LANGUAGE plpgsql AS $$
DECLARE
	grantee_oid oid;
	recipient text;
	table_grants text;
	sequence_grants text;
BEGIN
	-- NULL is tested beside the pattern rather than before it because
	-- `NULL !~ pattern` is NULL, not true: a body that asked only the regex
	-- would walk past a nameless owner into a CREATE SCHEMA whose name it did
	-- not have. Length is measured after the grammar, which is what makes
	-- length(owner) the same as octet_length(owner) — every character the
	-- grammar admits is one byte — and rejects the name before PostgreSQL
	-- shortens it, not after. A RAISE format string is a literal in plpgsql —
	-- `'a ' || 'b'` is a syntax error — which is why each message is one line.
	IF owner IS NULL OR owner !~ '^[a-z][a-z0-9_-]*$' THEN
		RAISE EXCEPTION 'platformkit_module_schema: % is not a module schema name; an owner must match ^[a-z][a-z0-9_-]*$', owner;
	END IF;
	IF length(owner) > 63 THEN
		RAISE EXCEPTION 'platformkit_module_schema: % is longer than one PostgreSQL identifier and would be silently truncated; the ledger would keep this name and pg_namespace would hold another', owner;
	END IF;
	IF owner = 'public' OR owner = 'information_schema' OR left(owner, 3) = 'pg_' THEN
		RAISE EXCEPTION 'platformkit_module_schema: % is a namespace the deployment or the catalog owns', owner;
	END IF;

	-- Owned by whoever runs the migration: CREATE SCHEMA needs CREATE on the
	-- database, which the role the runner connects as holds as database owner
	-- and the application role does not.
	EXECUTE format('CREATE SCHEMA IF NOT EXISTS %I', owner);

	-- The schema exists now either way, so this is where belonging is settled.
	-- GRANT USAGE and ALTER DEFAULT PRIVILEGES IN SCHEMA both need ownership or
	-- CREATE on the schema: reaching them against somebody else's namespace is
	-- the half-granted failure this function exists to prevent, so it raises and
	-- the file's transaction rolls back instead.
	IF (SELECT nspowner FROM pg_namespace WHERE nspname = owner) <> current_user::regrole THEN
		RAISE EXCEPTION 'platformkit_module_schema: % already exists and is owned by another role, which alone may grant into it', owner;
	END IF;

	-- The grantees, discovered and never named: the same CASE, the same
	-- aclexplode and the same skipping of the current role that kit/db's
	-- migrationLedger block applies to an object's own owner, one catalog
	-- earlier, and %I around the schema because the grammar admits characters an
	-- unquoted identifier would not survive. pg_default_acl is one row per (role,
	-- schema, object kind) holding an aclitem array, so a second call merges into
	-- the rows the first wrote and the count for the schema never moves.
	--
	-- defaclnamespace = current_schema()::regnamespace is the whole of the "no
	-- privilege the deployment did not hand out" guarantee, and it is wrong in
	-- both directions. Left out, a grantee's privilege types arrive from every
	-- namespace the migration role pinned defaults in and the function hands the
	-- union — a privilege that role holds in no namespace at all. Read as this
	-- schema's own OID instead, the loop finds nothing on a first call, because
	-- the deployment pinned its rows beside the namespace the module schema is
	-- opening next to, not inside a namespace that does not exist yet.
	-- current_schema() is that other namespace: the caller's search_path resolved
	-- by the server, public in the reference deployment and the test's own schema
	-- under dbtest. Losing that answer is the second thing a pinned search_path
	-- would cost, beside the one the header gives.
	--
	-- grantee_oid carries the OID rather than the decoded name, and is spelled
	-- apart from aclexplode's own grantee column for a reason. SQL name
	-- resolution does not prefer a plpgsql variable over a range table's column,
	-- so naming this one `grantee` would make `acl.grantee = grantee` two column
	-- references and one ambiguous name: measured, every call then dies with
	-- 42702 column reference "grantee" is ambiguous. Loud is the better half of
	-- that outcome — the quiet half would be comparing a grantee with itself and
	-- handing every grantee the union of the deployment's privileges — and a
	-- name that cannot collide is why nobody has to find out which half they
	-- drew. The name is decoded once per grantee, here, which is the only place
	-- the PUBLIC spelling is decided.
	FOR grantee_oid IN
		SELECT DISTINCT acl.grantee
		FROM pg_default_acl d CROSS JOIN LATERAL aclexplode(d.defaclacl) acl
		WHERE d.defaclrole = current_user::regrole
			AND d.defaclnamespace = current_schema()::regnamespace
			AND d.defaclobjtype IN ('r', 'S')
			AND acl.grantee <> d.defaclrole
	LOOP
		recipient := CASE WHEN grantee_oid = 0 THEN 'PUBLIC'
			ELSE quote_ident(pg_get_userbyid(grantee_oid)) END;
		EXECUTE 'GRANT USAGE ON SCHEMA ' || format('%I', owner) || ' TO ' || recipient;

		-- What that grantee holds by default, and nothing wider: aclexplode
		-- answers with privilege types spelled the way GRANT ... ON TABLES and
		-- GRANT ... ON SEQUENCES want them, so the row that named the grantee
		-- also names its rights. A grantee holding nothing for one of the two
		-- object kinds gets no statement for that kind, because GRANT with an
		-- empty privilege list is a syntax error. The namespace filter is repeated
		-- here and below rather than carried over from the loop, because these are
		-- separate queries and an aggregate that read another namespace would hand
		-- this grantee a privilege its own row never named. Grant options are not
		-- carried over: what arrives in a module's schema is no wider than what the
		-- deployment pinned for this namespace, and never more widely delegable
		-- than it was.
		SELECT string_agg(DISTINCT acl.privilege_type, ',' ORDER BY acl.privilege_type)
		INTO table_grants
		FROM pg_default_acl d CROSS JOIN LATERAL aclexplode(d.defaclacl) acl
		WHERE d.defaclrole = current_user::regrole
			AND d.defaclnamespace = current_schema()::regnamespace
			AND d.defaclobjtype = 'r' AND acl.grantee = grantee_oid;
		IF table_grants IS NOT NULL THEN
			EXECUTE 'ALTER DEFAULT PRIVILEGES IN SCHEMA ' || format('%I', owner)
				|| ' GRANT ' || table_grants || ' ON TABLES TO ' || recipient;
		END IF;

		SELECT string_agg(DISTINCT acl.privilege_type, ',' ORDER BY acl.privilege_type)
		INTO sequence_grants
		FROM pg_default_acl d CROSS JOIN LATERAL aclexplode(d.defaclacl) acl
		WHERE d.defaclrole = current_user::regrole
			AND d.defaclnamespace = current_schema()::regnamespace
			AND d.defaclobjtype = 'S' AND acl.grantee = grantee_oid;
		IF sequence_grants IS NOT NULL THEN
			EXECUTE 'ALTER DEFAULT PRIVILEGES IN SCHEMA ' || format('%I', owner)
				|| ' GRANT ' || sequence_grants || ' ON SEQUENCES TO ' || recipient;
		END IF;
	END LOOP;
END
$$;
