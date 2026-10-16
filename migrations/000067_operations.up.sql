-- Operation receipts are tenant rows; request retry claims remain system-only.
CREATE TABLE platformkit_operations (
 id uuid NOT NULL CHECK (id <> '00000000-0000-0000-0000-000000000000'),
 tenant_id uuid NOT NULL CHECK (tenant_id <> '00000000-0000-0000-0000-000000000000'),
 operation_id text NOT NULL CHECK (length(operation_id) BETWEEN 1 AND 128),
 caller_id uuid NOT NULL,
 person_id uuid,
 method text NOT NULL,
 route text NOT NULL,
 outcome text CHECK (outcome IN ('succeeded', 'failed', 'pending_approval')),
 revision bigint NOT NULL CHECK (revision > 0),
 started_at timestamptz NOT NULL,
 outcome_at timestamptz,
 deleted_at timestamptz,
 PRIMARY KEY (tenant_id, id),
 CHECK ((outcome IS NULL) = (outcome_at IS NULL)),
 CHECK (outcome_at IS NULL OR outcome_at >= started_at)
);
ALTER TABLE platformkit_operations ENABLE ROW LEVEL SECURITY;
ALTER TABLE platformkit_operations FORCE ROW LEVEL SECURITY;
CREATE POLICY platformkit_operations_scope ON platformkit_operations
 USING (platformkit_tenant_match(tenant_id)) WITH CHECK (platformkit_tenant_match(tenant_id));
CREATE INDEX platformkit_operations_expiry ON platformkit_operations (outcome_at)
 WHERE outcome IN ('succeeded', 'failed');
ALTER TABLE platformkit_idempotency ADD COLUMN operation_ref uuid, ADD COLUMN started_at timestamptz;
ALTER TABLE platformkit_idempotency ADD CONSTRAINT platformkit_idempotency_operation_pair
 CHECK ((operation_ref IS NULL) = (started_at IS NULL));
ALTER TABLE platformkit_outbox ADD COLUMN operation_id text, ADD COLUMN operation_ref uuid,
 ADD COLUMN operation_tenant_id uuid, ADD COLUMN operation_person_id uuid,
 ADD COLUMN operation_outcome text, ADD COLUMN operation_revision bigint;
ALTER TABLE platformkit_outbox ADD CONSTRAINT outbox_operation_complete CHECK (
 (operation_id IS NULL AND operation_ref IS NULL AND operation_tenant_id IS NULL
  AND operation_person_id IS NULL AND operation_outcome IS NULL AND operation_revision IS NULL)
 OR (operation_id IS NOT NULL AND operation_ref IS NOT NULL AND operation_tenant_id IS NOT NULL
  AND operation_ref <> '00000000-0000-0000-0000-000000000000'
  AND operation_tenant_id <> '00000000-0000-0000-0000-000000000000'
  AND length(operation_id) BETWEEN 1 AND 128
  AND operation_revision IS NOT NULL AND operation_revision > 0 AND (operation_outcome IS NULL OR operation_outcome IN ('succeeded','failed','pending_approval'))));
