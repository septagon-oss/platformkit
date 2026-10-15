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
-- here, for the answer that is nowhere else: the first uploaded event names the
-- visibility the bytes arrived under. Where a file has no such event — its upload
-- predates the ledger's own retention, or the row arrived by some path that published
-- nothing — there is no record of a choice to recover, and the row's own value is all
-- that survives of it: this refuses to take away a public read on the strength of a
-- missing row, which is the direction that can be corrected by a later revocation
-- rather than one that destroys the only evidence a page has.
--
-- From here nothing writes this column. A revocation puts files.visibility back to
-- private for a file whose recorded upload visibility is private (see Service.
-- settleAudience), so the two columns agree again for every row an audience was ever
-- taken back from, and the door reads this one for the half that is not derived.
ALTER TABLE files ADD COLUMN upload_visibility varchar(10) NOT NULL DEFAULT 'private';

UPDATE files AS f SET upload_visibility = COALESCE(u.visibility, f.visibility)
FROM (
	SELECT DISTINCT ON ((payload->>'fileId')::uuid)
		(payload->>'fileId')::uuid AS id,
		CASE payload->>'visibility'
			WHEN 'public' THEN 'public'
			WHEN 'private' THEN 'private'
		END AS visibility
	FROM platformkit_outbox
	WHERE name = 'file.uploaded'
		AND (payload->>'fileId') ~
			'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$'
	ORDER BY (payload->>'fileId')::uuid, created_at, id
) AS u
WHERE u.id = f.id;

ALTER TABLE files ADD CONSTRAINT files_upload_visibility
	CHECK (upload_visibility IN ('private', 'public'));
