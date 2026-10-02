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
-- It is append-only in the same sense audit_events is, and it is fenced the same way
-- with one difference: the application role may read this table and may not write it,
-- while the role that expires history may write it and holds no rights on anything else
-- here. That is the tree's third privilege shape — the runner's two tables are revoked
-- entirely (migrations/rls_test.go), ordinary module tables carry the deployment's
-- defaults, this one is "readable by everybody the tenant policy lets, writable by
-- nobody the application is" — which is why the sweep below REVOKEs everything and
-- grants exactly one privilege back, and why that one privilege is given to PUBLIC:
-- module SQL may name no role (migrations/review_round2_module_schema_grants_test.go),
-- and a read grant is the only grant whose correctness does not depend on knowing which
-- role the deployment picked. Writes are the deployment's to grant, beside the role it
-- names — apps/platformkit's ensureRetentionGrant and this README's Provisioning
-- section carry the two statements.
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
