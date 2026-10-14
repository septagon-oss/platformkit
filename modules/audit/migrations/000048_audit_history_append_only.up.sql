-- History stops being history when the role that writes it can also rewrite it.
--
-- Until this file, "append-only" was a statement about modules/audit's Go code: nothing
-- in the module issues an UPDATE and nothing removes a row but the retention job, and
-- that was all there was. The role the application connects as held UPDATE and TRUNCATE
-- on this table through whatever the deployment pinned for it — apps/platformkit/
-- postgres-init.sql and kit/db/dbtest both pin SELECT, INSERT, UPDATE, DELETE to every
-- table as it appears — and the core review of 2026-09-29 (P1) walked through that door:
-- it recorded an event through the real audit service, then UPDATEd and DELETEd the row
-- through ordinary tenant transactions. A read-only HTTP API is not append-only storage.
--
-- Two halves, and each is for a case the other cannot see:
--
--   * The REVOKE below is what refuses the application today, in PostgreSQL's own
--     vocabulary (SQLSTATE 42501) before any PL/pgSQL runs. It discovers its grantees
--     from the catalog rather than naming a role, because a module may not name a role
--     (migrations/module_schema_grants_test.go) and because the point is
--     "whatever the deployment handed out, take it back", not "take it back from the
--     one login I guessed". kit/db/migrate.go's own ledger sweep is the shape, for the
--     same reason: an installation pins its own privileges and this file cannot know them.
--
--   * The triggers are what refuses the application after an operator runs
--     GRANT ALL ON ALL TABLES IN SCHEMA public TO <the app role>, which is the single
--     most likely thing a tired operator does at 3 a.m. They also refuse the roles the
--     REVOKE says nothing about: the table's owner, and a superuser, both of which hold
--     every privilege by definition and cannot be refused by a grant.
--
-- Four triggers, one per way a row stops being what it was: UPDATE rewrites one, the
-- UPDATE trigger refuses it; DELETE removes one, the DELETE row trigger admits only the
-- expiry shape and the DELETE statement trigger refuses an expiry by a role the deployment
-- gave an append door beside the trail; TRUNCATE removes every row of every tenant at once,
-- and the TRUNCATE trigger refuses it outright. The last is the one no REVOKE can be trusted for, twice
-- over: it is refused by a grant the operator can hand back with one statement, and it
-- ignores row-level security, so the one write that reaches across tenants is exactly
-- the one a privilege list alone would have to catch. PostgreSQL lets a BEFORE TRUNCATE
-- trigger fire FOR EACH STATEMENT and nothing else — no row trigger can see a TRUNCATE,
-- which is why the row triggers below are not enough and why this one is a statement
-- trigger beside them rather than a fourth case inside them. It fires before the rows
-- are touched, so a refused TRUNCATE leaves the trail exactly where it was.
--
-- UPDATE is refused outright. DELETE is refused except through one door, and the door
-- is a shape rather than an identity: a role that may append to the trail may never
-- expire it, and a role that may not append may expire only rows past the floor. The
-- application must hold INSERT or it cannot write history at all, so it can never
-- satisfy the shape — that is why its DELETE privilege, revoked or not, opens nothing.
-- Revoking DELETE as well was the other option and it is the wrong one: the retention
-- job runs on the application's connection, so revoking it would leave the expiry with
-- no door except a role the application is not, which is what 000049 and Deps.RetainURL
-- deliver; and leaving DELETE to a GUC, a session setting or a marker row the
-- application could write would be a door the fenced-in role can open — platformkit_app
-- already sets platformkit.system_access for itself today (a measurement this delivery
-- made before choosing), which is why platformkit.tenant_id is guarded by
-- scripts/check_gucs.sh and not by a privilege.
--
-- The floor, 365 days, is the kernel's minimum for forgetting audit history and is
-- spelled three times on purpose: here, in internal.retentionFloor, and in
-- config.DefaultRetentionDays — the same-numbers-far-apart convention this tree already
-- keeps for 365. A deployment that configures a shorter period is refused at boot by
-- config.Validate, not by a job that silently deletes nothing at three in the morning.
-- A deployment that must keep seven years needs no change: the floor is a minimum.
--
-- The residual, stated rather than implied: the application role may still *hold*
-- DELETE (this file does not revoke it) and has_table_privilege will answer true; what
-- it cannot do is use it. A role that can DROP TRIGGER can rewrite history — that is
-- the DDL role, outside this boundary by definition, exactly as postgres-init.sql's
-- header says of a superuser and the tenant policies. And nothing here makes history
-- unforgeable in a way a chain would: this table gains no prev_hash, and README.md's
-- Limits section carries the sentence that it is still owed.

-- Which clause is asked where is a cost rather than a taste: the trail's own privileges are
-- one catalog lookup and belong on the row, where the floor lives too, and the append doors a
-- deployment can build beside the trail are a walk over pg_depend and belong on the statement.
-- Both halves are measured in the header of audit_events_never_expired_through_a_door.
--
-- The bodies touch only pg_catalog (pg_trigger's OID, pg_depend, pg_rewrite, pg_class,
-- has_table_privilege, has_column_privilege, has_any_column_privilege, pg_attribute,
-- current_user), so the path is pinned: a caller's
-- search_path must not be able to resolve a different pg_class, a different
-- pg_attribute, or a different privilege function under it.
CREATE FUNCTION audit_events_never_rewritten() RETURNS trigger
	LANGUAGE plpgsql
	SET search_path = pg_catalog
AS $$
BEGIN
	-- insufficient_privilege is the SQLSTATE PostgreSQL answers a revoked privilege
	-- with, so whichever half catches a write, the error class is one answer.
	RAISE EXCEPTION 'audit history is append-only: %.% is never rewritten', TG_TABLE_SCHEMA, TG_TABLE_NAME
		USING ERRCODE = 'insufficient_privilege';
END
$$;

CREATE FUNCTION audit_events_expire_only_after() RETURNS trigger
	LANGUAGE plpgsql
	SET search_path = pg_catalog
AS $$
BEGIN
	-- Capability, not identity, and not a setting: may delete, may not append, and
	-- past the floor. The first condition and the append question together are what make the
	-- retention role a different role from the application rather than the application
	-- wearing a hat.
	IF has_table_privilege(current_user, TG_RELID, 'DELETE')
		AND NOT has_table_privilege(current_user, TG_RELID, 'INSERT')
		-- "May not append" is asked of every column and not only of the table. INSERT is
		-- grantable at either level and both are deployments an installation actually runs:
		-- GRANT INSERT ON audit_events, and GRANT INSERT (tenant_id, occurred_at, name,
		-- actor, event_id, payload, records, request_id, client_ip, traceparent) ON
		-- audit_events, which is what a least-privilege writer is handed when the trail's
		-- own defaults are to stay the table's. has_column_privilege reports a
		-- table-level grant through each of the table's columns too, so this one clause
		-- covers both grant shapes — and the table-only clause above does not, which is how
		-- a role with no table-level INSERT appended through the real service and then
		-- DELETEd its own row with no mark to say it happened (root's review of 2026-10-06,
		-- F1). One column right is enough to insert a row alongside its defaults, so the
		-- answer is "any column", not "every column"; and no deployment that grants INSERT
		-- by column can be fenced by a question that only asks about the table.
		--
		-- What these two clauses cannot see is an append made through a relation that draws its
		-- rows from the trail rather than one the trail granted INSERT to — a deployment's own
		-- view over it — because that question is not answered by this table's ACL or by any
		-- column of it. audit_events_never_expired_through_a_door asks it, once per DELETE
		-- statement: asked per row, the same question costs 1.1 ms a row measured — 23 s to
		-- expire 20 000 rows, where the same sweep asked once per statement costs what the
		-- fence without it costs (its header carries both numbers and how they were run).
		AND NOT EXISTS (
			SELECT 1 FROM pg_attribute col
			WHERE col.attrelid = TG_RELID AND col.attnum > 0 AND NOT col.attisdropped
				AND has_column_privilege(current_user, TG_RELID, col.attnum, 'INSERT')
		)
		AND OLD.occurred_at < now() - interval '365 days' THEN
		RETURN OLD;
	END IF;
	RAISE EXCEPTION 'audit history is append-only: % refuses an expiry by %', TG_TABLE_NAME, current_user
		USING ERRCODE = 'insufficient_privilege';
END
$$;

-- The append door a deployment can build beside the trail is asked about once per DELETE
-- statement, and not in the row trigger at all. An INSERT through a plain view resolves as the
-- view's owner — PostgreSQL's security_invoker option defaults to off — so a view over the trail
-- owned by the trail's owner writes history for a role the trail granted no INSERT at either
-- level, and the row trigger's two clauses read that role as an expiry role: it appended through
-- the deployment's own object, then DELETEd the row unmarked. What a role may append to the trail
-- is a fact about the deployment's objects, not about which catalog row its grant was written
-- against, so the walk follows the hinges pg_depend records between a rule and the relation it
-- reads, as far as they go — a view over a view is the same door with one more hinge in it — and
-- one INSERT right on any column of any of them opens it. internal.TestViewAppenderCannotExpireHistory
-- and internal.TestViewOverViewAppenderCannotExpireHistory are the two doors, one with each depth.
--
-- FOR EACH STATEMENT is the shape this costs, and the number is measured rather than assumed.
-- Expiring 20 000 aged rows in one DELETE on the supplied stack: the fence as it stood before this
-- trigger answers in 10 microseconds a row (183–279 ms for the sweep, nine runs), this same walk
-- asked per row answers in 1.1 milliseconds a row (22.7–27.4 s — a retention job that never
-- finishes), and asked once per statement it changes nothing an interleaved run can see, with a
-- view over the trail or without one (174–356 ms and 183–345 ms, nine runs each). A BEFORE
-- STATEMENT trigger fires before the first row trigger and before any row is touched, so a refused
-- expiry leaves the trail exactly where it was, and it refuses the retention job's own delete no
-- more often than the row trigger refuses it today: the same capability question, answered earlier
-- and a thousand times cheaper.
CREATE FUNCTION audit_events_never_expired_through_a_door() RETURNS trigger
	LANGUAGE plpgsql
	SET search_path = pg_catalog
AS $$
BEGIN
	-- Only the expiry shape is worth the walk: a role that cannot delete was refused by the
	-- privilege check that ran before this trigger fired, and one that may append directly is
	-- refused by the row trigger's clauses either way.
	IF has_table_privilege(current_user, TG_RELID, 'DELETE')
		AND EXISTS (
			WITH RECURSIVE append_door(oid) AS (
				SELECT drawn.ev_class
				FROM pg_depend hinge
				JOIN pg_rewrite drawn ON drawn.oid = hinge.objid
				JOIN pg_class door ON door.oid = drawn.ev_class AND door.relkind = 'v'
				WHERE hinge.classid = 'pg_rewrite'::regclass
					AND hinge.refclassid = 'pg_class'::regclass
					AND hinge.refobjid = TG_RELID
				UNION
				SELECT drawn.ev_class
				FROM pg_depend hinge
				JOIN pg_rewrite drawn ON drawn.oid = hinge.objid
				JOIN pg_class door ON door.oid = drawn.ev_class AND door.relkind = 'v'
				JOIN append_door drawn_from ON drawn_from.oid = hinge.refobjid
			)
			SELECT 1 FROM append_door door
			WHERE has_any_column_privilege(current_user, door.oid, 'INSERT')
		) THEN
		RAISE EXCEPTION 'audit history is append-only: % refuses an expiry by %: the role appends through a view over it',
			TG_TABLE_NAME, current_user
			USING ERRCODE = 'insufficient_privilege';
	END IF;
	-- A BEFORE trigger returns the row it would write and a statement-level one has no row;
	-- NULL is PostgreSQL's "carry on with the row the planner already has".
	RETURN NULL;
END
$$;

-- BEFORE UPDATE, so a rewrite is refused before the row is touched. FOR EACH ROW,
-- because a statement-level trigger fires after the rows are already gone and this
-- one exists to leave them where they are.
CREATE TRIGGER audit_events_never_rewritten
	BEFORE UPDATE ON audit_events
	FOR EACH ROW EXECUTE FUNCTION audit_events_never_rewritten();

-- BEFORE TRUNCATE, FOR EACH STATEMENT: the only shape PostgreSQL gives a TRUNCATE
-- trigger, and enough, because the refusal is about the statement and not about a row.
-- It is fired after the privilege check and before any row is removed, so GRANT ALL
-- buys an error instead of an empty table.
CREATE TRIGGER audit_events_never_emptied
	BEFORE TRUNCATE ON audit_events
	FOR EACH STATEMENT EXECUTE FUNCTION audit_events_never_rewritten();

-- BEFORE DELETE, FOR EACH STATEMENT: the door beside the trail, asked once for the whole
-- statement and before any row is touched. It is a statement trigger because the question is
-- about the deployment's objects rather than about one row, and because the same question asked
-- per row makes a retention sweep two thousand times the cost it needs to be.
CREATE TRIGGER audit_events_never_expired_through_a_door
	BEFORE DELETE ON audit_events
	FOR EACH STATEMENT EXECUTE FUNCTION audit_events_never_expired_through_a_door();

-- BEFORE DELETE: the append door and the expiry door are different doors, and an
-- expiry inside the floor is the same forgery in a slower costume.
CREATE TRIGGER audit_events_expire_only_after
	BEFORE DELETE ON audit_events
	FOR EACH ROW EXECUTE FUNCTION audit_events_expire_only_after();

-- Every grantee the catalog discovers, PUBLIC included when the deployment wrote it
-- into the ACL, and never the owner (REVOKE FROM the owner of a table it owns is a
-- no-op that reads as a decision). UPDATE and TRUNCATE are the two the finding is
-- about; REFERENCES and TRIGGER go with them because a trail a caller can build a
-- view or a constraint over is a trail with a second, rewritable copy of itself, and
-- because GRANT ALL is the thing this must survive. SELECT and INSERT are what the
-- application needs to read and append, and stay the deployment's to grant.
DO $$
DECLARE recipient text;
BEGIN
	FOR recipient IN
		SELECT DISTINCT CASE WHEN acl.grantee = 0 THEN 'PUBLIC'
			ELSE quote_ident(pg_get_userbyid(acl.grantee)) END
		FROM pg_class c CROSS JOIN LATERAL aclexplode(c.relacl) acl
		WHERE c.oid = 'audit_events'::regclass AND acl.grantee <> c.relowner
	LOOP
		EXECUTE 'REVOKE UPDATE, TRUNCATE, REFERENCES, TRIGGER ON TABLE audit_events'
			|| ' FROM ' || recipient || ' CASCADE';
	END LOOP;
END
$$;
