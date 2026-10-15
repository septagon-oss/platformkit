-- The grace-period clock: when this image stopped being shown to anybody.
--
-- It is not deleted_at, not updated_at and not borrowed from file_uses. The
-- ledger says which record shows a file and stops saying it when the body
-- changes; the question the release sweep asks is *when* the answer became
-- "nobody", and that moment is a fact about files, so it is stamped here, by
-- the one command that ends a use (Service.SetUses) and by nothing else.
-- An upload starts no clock: the rule releases a file whose last use *ended*,
-- and a file the ledger has never spoken about has no such moment to measure
-- from. Stamping the arrival reaped every image whose only name lives outside
-- the ledger -- site_settings names its logo by a bare uuid, files no use row
-- and places no hold -- so a configured logo was destroyed a day after a
-- person saved it. The unsaved upload is that boundary's cost, charged to the
-- quota and the retention sweep rather than to a rule that cannot tell an
-- unsaved picture from a configured one.
--
-- Only a file the image pass measured is ever stamped. The ledger speaks only
-- about pictures whose plan it filed, and "the pass measured it" is already
-- readable as width > 0: a PDF attached to a field has no uses, no ledger rows
-- and no business being reaped, so a rule that stamped every upload would delete
-- every document a day after it arrived. That is why the sweep's first predicate
-- is width > 0 and why the CHECK below refuses a stamp on an unmeasured row —
-- every stamp this column can carry came from the ledger having spoken, and a
-- file the ledger cannot speak about is left to a sweep that would be guessing.
ALTER TABLE files ADD COLUMN unused_since timestamptz;

ALTER TABLE files ADD CONSTRAINT files_unused_since_measured
	CHECK (unused_since IS NULL OR width > 0);

-- The last-use sweep's removal has to be able to certify its own bytes. The work
-- order carries the cause of the removal, the worker copies it into the proof
-- row, and the proof table's closed set is what a person reads the table for — so
-- a fourth cause widens it here, in the migration that creates the fourth cause,
-- rather than leaving a worker to be refused by the CHECK on the row it must write
-- ('unused' is 6 characters and the column is varchar(16)).
ALTER TABLE file_erasures DROP CONSTRAINT file_erasures_cause;
ALTER TABLE file_erasures ADD CONSTRAINT file_erasures_cause
	CHECK (cause IN ('expired', 'subject', 'caller', 'unused'));
