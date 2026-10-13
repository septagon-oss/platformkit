-- pkit: autocommit=true
-- The sweep's read, once a day: the oldest stamp past the grace period, a batch
-- at a time. Partial because a table of images in use looks like a table of NULLs
-- here, and an index over all of them would be a scan of everything anybody is
-- showing to find the handful nobody is.
CREATE INDEX CONCURRENTLY IF NOT EXISTS files_unused_since ON files (unused_since, id)
	WHERE unused_since IS NOT NULL AND width > 0;

