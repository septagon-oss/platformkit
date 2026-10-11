-- Demo is chosen at tenant creation and read for every demo seed request.
-- Existing tenants remain non-demo; no command changes this column later.
ALTER TABLE tenants ADD COLUMN demo boolean NOT NULL DEFAULT false;
