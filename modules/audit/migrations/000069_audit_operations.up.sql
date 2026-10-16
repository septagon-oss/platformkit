-- Preserve originating intent independently of the executing actor.
ALTER TABLE audit_events ADD COLUMN operation_id text, ADD COLUMN operation_ref uuid,
 ADD COLUMN operation_tenant_id uuid, ADD COLUMN operation_person_id uuid,
 ADD COLUMN operation_outcome text, ADD COLUMN operation_revision bigint;
