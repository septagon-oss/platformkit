-- What a picture says about itself, and the claim that it says nothing.
--
-- Alt text belongs to the file and not to the record that shows it: the same
-- photograph appears in four pages, and a caption typed into each of them is
-- four captions that disagree the moment one is corrected. It is also the only
-- accessibility contract a file can keep on its own — a body that embeds an
-- image inherits whatever the image carries.
--
-- Both columns have a DEFAULT, so neither rewrites the table or refuses a write
-- while it does, and the default '' is the whole backfill: a caption invented by
-- a migration would be a caption nobody wrote. The empty-but-not-decorative
-- state is therefore reachable — every upload and every seeded file sits in it —
-- and it is the state the library will not hand a body out of, which is a
-- command's decision (Service.SetAlt, contracts.AltClaim) rather than a
-- column's.
--
-- What the column does hold is the contradiction: a picture cannot both carry
-- words and declare that it carries nothing a reader would want. The CHECK is
-- the entity's own rule (File.Validate) written where a hand-written statement
-- cannot step around it.
ALTER TABLE files ADD COLUMN alt varchar(1000) NOT NULL DEFAULT '';

ALTER TABLE files ADD COLUMN decorative boolean NOT NULL DEFAULT false;

ALTER TABLE files ADD CONSTRAINT file_files_decorative
	CHECK (decorative = false OR alt = '');
