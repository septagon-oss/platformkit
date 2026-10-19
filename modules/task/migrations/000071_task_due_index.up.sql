-- pkit: autocommit=true
-- The index behind "how many of this tenant's tasks are due today".
--
-- The question is asked as a card, on the screen a person opens first, and it is
-- asked of one tenant's open work: due_at is nullable and most of a long-lived
-- workspace's history has none, so the index is partial and covers only the rows
-- that can answer it. Without it the count reads the tenant's whole task set,
-- which is the one table in this installation that grows without ever shrinking.
--
-- One statement per file, and CONCURRENTLY, because tasks already exists: the
-- plain build takes a SHARE lock over a table every desk in the installation
-- writes. See migrations/README.md's autocommit row.
CREATE INDEX CONCURRENTLY IF NOT EXISTS tasks_tenant_due
	ON tasks (tenant_id, due_at) WHERE deleted_at IS NULL AND due_at IS NOT NULL;
