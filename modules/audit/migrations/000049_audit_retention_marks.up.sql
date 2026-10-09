-- What the retention job removed, written by the job itself.
--
-- "The retention path is itself audited" has one honest shape and one flattering one.
-- The flattering one is an event: the trail is built from events, so an expiry could
-- publish one and be on the trail like anything else. It cannot. modules/audit declares
-- no event and module.go's comment says why — an audit of audits is a loop — and worse,
-- an expiry event would be relayed into the very table the relay is trimming, so the
-- act of forgetting would add a row to what is being forgotten.
--
-- So this table is the record: tenant, the cutoff applied, how many rows went, and when
-- the pass ran. It is a record and not a seal — the role that writes it is the role that
-- deletes, and nothing in the trail's coverage verdict (§Coverage) depends on trusting
-- it. What it buys is arithmetic a reader can check: every row the trail holds, plus
-- every row a mark says went, is what the outbox published, and a pass that removed
-- more than the floor allowed is visible here without anyone diffing a table's row count
-- by hand.
--
-- It is append-only in the same sense audit_events is, and it is fenced the same way,
-- with one difference: the application role may read this table and may not write it,
-- while the role that expires history may write it and holds no rights on anything else
-- here. "The same way" is the triggers, not only the grants, and this table needs its
-- own pair for the same reason the trail does: the sweep below takes back whatever the
-- deployment handed out, and an operator's GRANT ALL hands it straight back — so the
-- rewrite, the delete and the TRUNCATE are refused by a trigger that fires whoever runs
-- it, the table's owner included. The one write the job must keep is INSERT: a mark is
-- written once, describes one removal, and is never amended, so a row trigger on each of
-- the three other verbs costs nothing this table needs. That is the tree's third
-- privilege shape — the runner's two tables are revoked entirely
-- (migrations/rls_test.go), ordinary module tables carry the deployment's defaults,
-- this one is "readable by everybody the tenant policy lets, writable by nobody the
-- application is" — which is why the sweep below REVOKEs everything and
-- grants exactly one privilege back, and why that one privilege is given to PUBLIC:
-- module SQL may name no role (migrations/review_round2_module_schema_grants_test.go),
-- and a read grant is the only grant whose correctness does not depend on knowing which
-- role the deployment picked. Writes are the deployment's to grant, beside the role it
-- names — this README's Provisioning section carries the two statements.
--
-- No foreign key, and none may be added: the rows a mark describes are the rows it
-- removed, so an ON DELETE CASCADE path into this table would be a delete door around
-- the trigger that guards audit_events, and a mark whose evidence rows are gone says
-- what went anyway.
CREATE TABLE audit_retention_marks (
	id        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id uuid NOT NULL,
	-- Rows with occurred_at before this were removed. The cutoff is the database's
	-- now() minus the configured period, computed inside the delete's own transaction,
	-- so two workers whose clocks have drifted record the same boundary they applied.
	cutoff    timestamptz NOT NULL,
	removed   bigint NOT NULL,
	ran_at    timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE audit_retention_marks ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_retention_marks FORCE ROW LEVEL SECURITY;

CREATE POLICY audit_retention_marks_tenant ON audit_retention_marks
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));

-- The trail's own index over the removals, because the question a reader asks of this
-- table is "when did this tenant's trail last forget, and how much", which is one
-- tenant's marks in order.
CREATE INDEX audit_retention_marks_tenant_time ON audit_retention_marks (tenant_id, ran_at DESC);

-- A mark is evidence, and evidence that can be edited is not evidence: the count a mark
-- carries is the arithmetic a reader checks the trail against (see the header), and a
-- pass that removed too much would fix its own number rather than be caught by it. One
-- function for the three verbs, because its body says nothing about any of them —
-- TG_TABLE_SCHEMA, TG_TABLE_NAME and TG_OP say it — and three triggers, because
-- PostgreSQL fires a TRUNCATE trigger no other way than FOR EACH STATEMENT.
CREATE FUNCTION audit_retention_marks_never_rewritten() RETURNS trigger
	LANGUAGE plpgsql
	SET search_path = pg_catalog
AS $$
BEGIN
	-- The same SQLSTATE as the trail's triggers, so a rewrite of a record and a rewrite
	-- of the trail it accounts for fail as one class of mistake.
	RAISE EXCEPTION 'audit retention marks are append-only: % refuses a %', TG_TABLE_NAME, TG_OP
		USING ERRCODE = 'insufficient_privilege';
END
$$;

CREATE TRIGGER audit_retention_marks_never_rewritten
	BEFORE UPDATE ON audit_retention_marks
	FOR EACH ROW EXECUTE FUNCTION audit_retention_marks_never_rewritten();

CREATE TRIGGER audit_retention_marks_never_removed
	BEFORE DELETE ON audit_retention_marks
	FOR EACH ROW EXECUTE FUNCTION audit_retention_marks_never_rewritten();

CREATE TRIGGER audit_retention_marks_never_emptied
	BEFORE TRUNCATE ON audit_retention_marks
	FOR EACH STATEMENT EXECUTE FUNCTION audit_retention_marks_never_rewritten();

-- Every grantee the catalog discovers, PUBLIC included when the deployment wrote it
-- into the ACL, and never the owner. Everything goes, and the read comes back below: a
-- table the application may read and may not write is the shape the header argues for,
-- and it is pinned by a grant rather than by the absence of one an upgrade could undo.
DO $$
DECLARE recipient text;
BEGIN
	FOR recipient IN
		SELECT DISTINCT CASE WHEN acl.grantee = 0 THEN 'PUBLIC'
			ELSE quote_ident(pg_get_userbyid(acl.grantee)) END
		FROM pg_class c CROSS JOIN LATERAL aclexplode(c.relacl) acl
		WHERE c.oid = 'audit_retention_marks'::regclass AND acl.grantee <> c.relowner
	LOOP
		EXECUTE 'REVOKE ALL ON TABLE audit_retention_marks FROM ' || recipient || ' CASCADE';
	END LOOP;
END
$$;

GRANT SELECT ON TABLE audit_retention_marks TO PUBLIC;
