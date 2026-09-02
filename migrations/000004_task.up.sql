-- Tasks: assignable, SLA-tracked work items. The entity is
-- modules/task/contracts/task.go and the columns below are its fields; the
-- module owns the SQL, the ledger is shared, and the file lives here because
-- there is one migration directory (ARCHITECTURE.md, idea 7).

CREATE TABLE tasks (
	id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id    uuid NOT NULL,
	created_at   timestamptz NOT NULL DEFAULT now(),
	updated_at   timestamptz NOT NULL DEFAULT now(),
	deleted_at   timestamptz,

	title        varchar(200) NOT NULL,
	description  text NOT NULL DEFAULT '',
	status       varchar(20) NOT NULL DEFAULT 'open',
	priority     varchar(20) NOT NULL DEFAULT 'normal',
	source       varchar(40) NOT NULL DEFAULT '',
	source_ref   varchar(120) NOT NULL DEFAULT '',
	assignee_id  uuid,
	due_at       timestamptz,
	sla_deadline timestamptz,
	sla_breached boolean NOT NULL DEFAULT false,
	resolved_at  timestamptz,
	resolution   text NOT NULL DEFAULT ''
);

-- The queries a task list actually makes: the default page, a queue by state, a
-- queue by urgency, one person's work, and the SLA sweep's overdue set. Each is
-- prefixed with tenant_id, because every one of them runs under a policy that
-- has already narrowed the table to one tenant, and an index that does not say
-- so makes Postgres read the other tenants' entries in order to discard them.
CREATE INDEX tasks_tenant_created  ON tasks (tenant_id, created_at DESC, id)
	WHERE deleted_at IS NULL;
CREATE INDEX tasks_tenant_status   ON tasks (tenant_id, status);
CREATE INDEX tasks_tenant_priority ON tasks (tenant_id, priority);
CREATE INDEX tasks_tenant_assignee ON tasks (tenant_id, assignee_id);
-- Partial, because the sweep only ever asks about the tasks that can still
-- breach: the index is the size of the open SLA clock, not of the history.
CREATE INDEX tasks_tenant_sla_deadline ON tasks (tenant_id, sla_deadline)
	WHERE sla_breached = false AND resolved_at IS NULL AND deleted_at IS NULL;

-- The shape every tenant-owned table has; migrations/000001 explains it. FORCE
-- is what binds the owner, and without it the application role escapes the
-- policy on any table it created itself.
ALTER TABLE tasks ENABLE ROW LEVEL SECURITY;
ALTER TABLE tasks FORCE ROW LEVEL SECURITY;

CREATE POLICY tasks_tenant ON tasks
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
