-- Pages count their own writes, so "has this page moved since the version was
-- read" is a question one SELECT answers: modules/history numbers a version with
-- the row's own count (000055's version column), and a restore that quotes a
-- number the row is past is refused with 412 and writes nothing. A second
-- counter, kept by the history module, would be a number that could disagree
-- with the page it describes, and the refusal would name neither.
--
-- The same column modules/task (000050) and modules/site (000039) grew for the
-- same reason, with the same four properties: the row counts its own writes, the
-- number is the row's and not another module's, nothing outside the two owners
-- below moves it, and every Go write of the row names the column in its own
-- crud.Update list — the generated PATCH through rest.Spec.bumpRevision, and the
-- three lifecycle commands in internal/service.go — because a revision a trigger
-- moved behind the kernel's back would be a stale-base refusal nobody could
-- explain to the person who got it.
--
-- Existing rows start at 1 rather than 0: a revision of 0 would mean "never
-- written", which is what the absence of the row already means, and a page
-- somebody has already edited is a page whose next restore is quoted against a
-- number. One statement, no backfill pass: the default writes the value. The
-- column inherits the table's existing forced row-level security and its
-- contents policy; no new table and so no new policy.
ALTER TABLE contents
	ADD COLUMN revision bigint NOT NULL DEFAULT 1
		CONSTRAINT contents_revision CHECK (revision >= 1);
