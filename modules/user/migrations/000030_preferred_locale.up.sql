-- A person's durable language choice. Empty means the browser and tenant
-- default choose; the revision makes concurrent changes explicit.
ALTER TABLE users
    ADD COLUMN preferred_locale text NOT NULL DEFAULT '',
    ADD COLUMN locale_revision bigint NOT NULL DEFAULT 0 CHECK (locale_revision >= 0);
