-- Who references this file, and from which record's field, in which locale.
--
-- One row per (record, field, locale, file), written in the writer's own
-- transaction by Service.SetUses — the kit calls it for every widget:richtext
-- field, so a use exists exactly when a body that names the file was accepted,
-- and never when it was not. That is what the release sweep reads: a file whose
-- last use ended is a file nobody is showing, and without this table the only
-- honest answer about whether a file is in use is "unknown, keep it forever".
--
-- The rewrite is in place and hard — no soft-deleted row lingers — because the
-- set of rows is the whole answer to "what uses this", and a row that survives
-- the body that stopped naming it says a record is displaying a picture it
-- dropped. The clock the sweep will read is therefore stamped by that sweep on
-- files and not borrowed from deleted_at here, which nothing writes.
CREATE TABLE file_uses (
	id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id    uuid NOT NULL,
	created_at   timestamptz NOT NULL DEFAULT now(),
	updated_at   timestamptz NOT NULL DEFAULT now(),
	deleted_at   timestamptz,

	file_id      uuid NOT NULL,
	module       varchar(32) NOT NULL,
	entity       varchar(64) NOT NULL,
	record       uuid        NOT NULL,
	field        varchar(64) NOT NULL,
	locale       varchar(35) NOT NULL,

	-- One row per thing that reads the file. Record alone is not the key: a
	-- record uses a file from its body and from its summary at once, and the
	-- two end at different moments.
	CONSTRAINT file_uses_use UNIQUE (tenant_id, module, entity, record, field, locale, file_id),
	CONSTRAINT file_uses_module CHECK (module <> ''),
	CONSTRAINT file_uses_entity CHECK (entity <> ''),
	-- lower(field) is the shape a widget name always arrives in; a CHECK on the
	-- shape is what a nameless or half-nameled field is refused by, in the
	-- database as well as in Validate.
	CONSTRAINT file_uses_field CHECK (field <> '' AND field = lower(field)),
	CONSTRAINT file_uses_locale CHECK (locale <> '')
);

-- The two reads there are: a file's own uses, which the details panel asks for,
-- and one record's rows, which SetUses rewrites and which a record's delete ends
-- through kit/rest, in the transaction that removes the row. Both are narrow,
-- both lead with tenant_id because that is what the policy filters on, and
-- neither is worth a write cost without a reader.
CREATE INDEX file_uses_file ON file_uses (tenant_id, file_id);
CREATE INDEX file_uses_record ON file_uses (tenant_id, module, entity, record);

-- The same policy files has had since 000019, and it is the reason a use of
-- another tenant's file cannot be written rather than cannot be read: the
-- WITH CHECK half refuses the row, and the USING half makes the file the row
-- would name unfindable in the first place.
ALTER TABLE file_uses ENABLE ROW LEVEL SECURITY;
ALTER TABLE file_uses FORCE ROW LEVEL SECURITY;
CREATE POLICY file_uses_tenant ON file_uses
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
