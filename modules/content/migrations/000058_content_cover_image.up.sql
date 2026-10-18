-- A page's cover is a file id with no foreign key: modules/content never names
-- modules/file, and a picture that has been deleted is a page that renders
-- without one. Nullable, no default and no backfill, so the ADD rewrites no row
-- and holds no write lock over a populated table. The row-level security policy
-- is per table over tenant_id, so a new column needs no policy of its own — what
-- keeps a foreign id out of a page is the file ledger's own tenant-scoped lookup,
-- run inside the transaction that writes this column.
ALTER TABLE contents ADD COLUMN cover_image_id uuid;
