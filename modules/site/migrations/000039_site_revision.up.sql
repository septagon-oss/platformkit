-- Site settings carry their own revision, so "has this row moved since the diff
-- was made" is a question one SELECT answers.
--
-- The number is the settings row's, not another module's: modules/change keeps
-- the base revision a proposal was made against and compares it to whatever this
-- column says when the apply locks the row. A diff set against revision 3 whose
-- subject is on 5 is refused, and the sentence says both numbers — which is only
-- an answer because the row counts its own writes.
--
-- Existing rows start at 1 rather than 0: Service.Save moves the number every
-- time it writes something a reader could see, and a revision of 0 would mean
-- "never written", which is what the absence of a row already means.
ALTER TABLE site_settings
	ADD COLUMN revision bigint NOT NULL DEFAULT 1
		CONSTRAINT site_settings_revision CHECK (revision >= 1);
