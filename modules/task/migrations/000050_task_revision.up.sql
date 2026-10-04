-- Tasks carry their own revision, so "has this row moved since the diff was
-- made" is a question one SELECT answers, and a task can be a change subject at
-- all: modules/change's contracts.Subject.Lock returns a document *and* a number,
-- and Apply refuses unless the number it re-reads under FOR UPDATE is the number
-- the diff was made against.
--
-- The same column modules/site grew for the same reason (000039), and the same
-- four properties: the row counts its own writes, the number is the row's and not
-- another module's, and nothing outside modules/task moves it. Every Go write of
-- the row adds the column to its own crud.Update list — the generated PATCH, each
-- of the three commands, and Writer.Save — because a revision a trigger moved
-- behind the kernel's back would be a stale-base refusal nobody could explain.
--
-- Existing rows start at 1 rather than 0: a revision of 0 would mean "never
-- written", which is what the absence of the row already means, and a proposal
-- made against an existing task is made against a row somebody already wrote.
-- One statement, no backfill pass: the default writes the value. The column
-- inherits the table's existing forced row-level security and its tasks_tenant
-- policy; no new table and so no new policy.
ALTER TABLE tasks
	ADD COLUMN revision bigint NOT NULL DEFAULT 1
		CONSTRAINT tasks_revision CHECK (revision >= 1);
