-- What the upload itself was given, kept where no lifecycle command rewrites it.
--
-- Who may read a file has two halves. One is the audience its upload was given; the
-- other is the audience a record with a public face asserts while it shows the file.
-- Only the first is a fact about the file, and until here it shared a column with the
-- second: 000056 describes Service.SetShown writing its derived answer — 'public' for
-- anything a published page was showing — straight into files.visibility, so for every
-- file a release ever published that column said 'public' whatever the person holding
-- file:manage had chosen at the upload. The asserted half has since moved to the use
-- row that asserts it (file_uses.public), and this column takes the given half back
-- out of the column that was being overwritten with the answer.
--
-- The rows an earlier release already overwrote are the reason this is a new column
-- rather than a fix to the old one: their files.visibility no longer says anything
-- about the upload, and the one place that ever recorded the upload's own choice is
-- the file.uploaded event, whose payload carries it. So the ledger is asked once,
-- here, for the answer that is nowhere else: each row starts from its own value, and
-- the first uploaded event of a file overwrites that with the visibility the bytes
-- actually arrived under. A file with no such event — its upload predates the ledger's
-- own retention, or the row arrived by some path that published nothing — keeps the
-- value it has, because there is no record of a choice to recover and a default of its
-- own would be one: this refuses to take away a public read on the strength of a
-- missing row, which is the direction that can be corrected by a later revocation
-- rather than one that destroys the only evidence a page has.
--
-- From here nothing writes this column. A revocation puts files.visibility back to
-- private for a file whose recorded upload visibility is private (see Service.
-- settleAudience), so the two columns agree again for every row an audience was ever
-- taken back from, and the door reads this one for the half that is not derived.
ALTER TABLE files ADD COLUMN upload_visibility varchar(10) NOT NULL DEFAULT 'private';

-- Each row starts from its own answer, so a file the ledger has no upload event for
-- keeps the read it has rather than losing one to a column nothing ever filled.
UPDATE files SET upload_visibility = visibility;

-- The events then correct the rows they can speak for: one row per fileId, the
-- earliest of them, which is the upload itself and not a later flip of the audience.
-- The key is compared as the text the event stored, so a payload naming anything that
-- is not this row's uuid simply says nothing, and the row keeps what it has.
UPDATE files AS f SET upload_visibility = u.visibility
FROM (
	SELECT DISTINCT ON (payload->>'fileId')
		payload->>'fileId' AS file_id,
		CASE payload->>'visibility'
			WHEN 'public' THEN 'public'
			WHEN 'private' THEN 'private'
		END AS visibility
	FROM platformkit_outbox
	WHERE name = 'file.uploaded'
	ORDER BY payload->>'fileId', created_at, id
) AS u
WHERE u.file_id = f.id::text AND u.visibility IS NOT NULL;

ALTER TABLE files ADD CONSTRAINT files_upload_visibility
	CHECK (upload_visibility IN ('private', 'public'));
