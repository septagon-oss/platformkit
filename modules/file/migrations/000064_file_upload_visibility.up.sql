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
-- about the upload. The one place that ever recorded the upload's own choice is the
-- file.uploaded event, whose payload carries it — and the outbox keeps a published row
-- for one week (kit/events/internal/delivery.Keep, purged by events.Purge), so on an
-- installation older than that most rows have no event left to ask. A row therefore
-- starts from the narrowest answer its own evidence supports, and the evidence decides
-- in this order:
--
--   * an upload event survives: the visibility its bytes actually arrived under, the
--     only answer that records a choice rather than inferring one;
--   * no event, and a live public use row explains the row's public: private, because
--     that public is the audience the showing record asserts, which lives in the use
--     row that asserts it, and the file stays anonymously readable for exactly as long
--     as that row stands. Writing the public here instead would keep a stranger's read
--     alive after the record took its own assertion back — which is the one direction
--     this release must never take, because the release it replaces made such a file
--     private at that moment;
--   * no event, and no public use row explains it: the value the row carries. A row
--     nothing public shows was never rewritten by anything but its upload, and the site
--     logo named by a bare uuid is exactly that row; defaulting it to private would
--     take a read away on the strength of a record that is merely missing.
--
-- The first two leave the answer a reader is given today where the previous release
-- left it — public for a file a public record shows, private for one it does not — and
-- disagree only about the moment that record takes its audience back, which is where
-- the previous release made the file private too. Only the third can keep a public it
-- cannot name an uploader for, and it is the case the previous release never rewrote.
--
-- From here nothing writes this column. A revocation puts files.visibility back to
-- private for a file whose recorded upload visibility is private (see Service.
-- settleAudience), so the two columns agree again for every row an audience was ever
-- taken back from, and the door reads this one for the half that is not derived.
ALTER TABLE files ADD COLUMN upload_visibility varchar(10) NOT NULL DEFAULT 'private';

-- The row's own evidence, before the ledger is asked: a public a live public use row
-- accounts for is that record's audience and not this file's, and nothing else moves.
-- The column arrives 'private' with every row, so the only rows this writes are the
-- ones that keep a public no use row explains — the write stays the size of the answer.
-- The use is matched by tenant as well as by file, which is what the row-level security
-- of both tables already promises and this statement should not have to rely on.
UPDATE files AS f SET upload_visibility = 'public'
	WHERE f.visibility = 'public' AND NOT EXISTS (
		SELECT 1 FROM file_uses u
		WHERE u.tenant_id = f.tenant_id AND u.file_id = f.id
			AND u.public AND u.deleted_at IS NULL);

-- The events then correct the rows they can speak for: one row per file, the
-- earliest of them, which is the upload itself and not a later flip of the audience.
-- The key is compared as the text the event stored, within the tenant that stored it,
-- so a payload naming anything that is not this row is simply no evidence, and the row
-- keeps what the statement above left it.
UPDATE files AS f SET upload_visibility = u.visibility
FROM (
	SELECT DISTINCT ON (tenant_id, payload->>'fileId')
		tenant_id,
		payload->>'fileId' AS file_id,
		CASE payload->>'visibility'
			WHEN 'public' THEN 'public'
			WHEN 'private' THEN 'private'
		END AS visibility
	FROM platformkit_outbox
	WHERE name = 'file.uploaded'
	ORDER BY tenant_id, payload->>'fileId', created_at, id
) AS u
WHERE u.file_id = f.id::text AND u.tenant_id = f.tenant_id AND u.visibility IS NOT NULL;

ALTER TABLE files ADD CONSTRAINT files_upload_visibility
	CHECK (upload_visibility IN ('private', 'public'));
