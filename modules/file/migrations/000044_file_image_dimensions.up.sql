-- What the server learned about an image's bytes.
--
-- One file, transactional, no down file (house rule): two ADD COLUMNs with a
-- default are metadata-only on Postgres 11+, so a deployment holding a hundred
-- million files is not rewritten by an upgrade that measures them.
--
-- Both numbers are the server's account of what arrived, which is why no
-- command takes them and no form field carries them: internal/image.go writes a
-- re-encoded frame whose dimensions it read out of the decoded pixels, and this
-- is the row that says what it measured. A file the pass did not run over —
-- a PDF, a ZIP, a raster no decoder reads — keeps 0 x 0, and 0 means nobody
-- measured rather than that the frame is a point.

ALTER TABLE files ADD COLUMN width int NOT NULL DEFAULT 0;
ALTER TABLE files ADD COLUMN height int NOT NULL DEFAULT 0;

-- A frame is unmeasured (both zero) or measured (both above zero). A width with
-- no height is a number nobody read, and the same shape is refused in Go by
-- File.Validate so a mistake is a 422 that says which field rather than a
-- constraint violation raised as a 500.
ALTER TABLE files DROP CONSTRAINT IF EXISTS files_dimensions;
ALTER TABLE files ADD CONSTRAINT files_dimensions
	CHECK (width >= 0 AND height >= 0 AND (width = 0 OR height > 0));

-- No index, and the reason is the same one the retention file gave for kind:
-- nothing reads these two columns as a filter. The rich-text port reads one
-- row's pair by primary key, which is the cheapest join there is, and a screen
-- that ever sorts a library by aspect ratio is a migration with its own
-- CONCURRENTLY file, not a write cost on every upload from now on.
