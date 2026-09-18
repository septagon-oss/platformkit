-- A handle: what a person is called inside one tenant.
--
-- The uuid in users.id is not wrong and stays. It is what every foreign key in
-- this database points at, what every event names as its subject, and what the
-- audit trail already recorded — so "who did this" survives any amount of
-- renaming precisely because it never stored a name. Re-keying on the handle
-- would make a rename a rewrite of history, and a person who changes their handle
-- would take their own trail with them.
--
-- What the handle adds is the identifier a human can type, say and remember. It
-- is per tenant, which is what the tenant column already means here: two tenants
-- can each have a `sam`, and neither is the other's. It is nullable, because a
-- handle is *claimed*, and nobody who was invited before this column existed has
-- claimed one.

ALTER TABLE users ADD COLUMN handle text;

-- Compared without case, as the address already is, and partial.
--
-- Unclaimed is the empty string and not NULL, because that is how this table
-- already spells an optional piece of text — see display_name — and a column with
-- two ways to mean "nobody" is a column where `IS NOT NULL` is wrong half the
-- time. Both spellings are excluded below, so a thousand unclaimed people are not
-- a thousand collisions on one empty string. The first version of this file checked
-- only NULL and every insert of an invited user failed the constraint; the existing
-- service tests found it, not this comment.
CREATE UNIQUE INDEX users_tenant_handle
	ON users (tenant_id, lower(handle))
	WHERE handle IS NOT NULL AND handle <> '' AND deleted_at IS NULL;

-- The last line of a rule the entity also enforces (contracts/user.go
-- Validate), and the only one that survives a bad deploy or a psql session: three
-- to thirty-two characters, lower-case letters, digits and interior . _ -, never
-- leading or trailing punctuation. Case is not checked here because the entity
-- folds it before it arrives, the same way email does.
ALTER TABLE users ADD CONSTRAINT users_handle_form
	CHECK (handle IS NULL OR handle = '' OR handle ~ '^[a-z0-9][a-z0-9._-]{1,30}[a-z0-9]$');

-- The lookup: `@ada` on a request that named nobody by uuid. Same shape as the
-- login index, same partial condition, so an unclaimed handle is not a row.
CREATE INDEX users_tenant_handle_lookup
	ON users (tenant_id, lower(handle))
	WHERE handle IS NOT NULL AND handle <> '' AND deleted_at IS NULL;
