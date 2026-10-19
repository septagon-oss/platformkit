-- Every version of a record's content, one row per committed write.
--
-- This is a content trail and not the audit trail. modules/audit answers "who did
-- what to anything" from an append-only table whose payloads are stored verbatim
-- and never compared to a successor; a restore needs the value one write left, in
-- one language, addressed by the number the record itself carries, and the audit
-- table holds none of those three. The two are read together and written by the
-- same transaction: this table is what a person compares, audit is what an
-- auditor cites.
--
-- A version is identified by (tenant, module, entity, record, locale, version),
-- and `version` is the record's own write count after the write that made this
-- row — the same number a conditional write quotes (entity.RevisionField). No
-- second counter exists here, which is what makes the UNIQUE constraint below a
-- statement about the record rather than a promise about this table's arithmetic.
--
-- No row is ever UPDATEd. The only DELETE is the retention sweep below, which
-- removes versions the rule no longer keeps; the content of a version that a
-- person restored is copied forward into a new row, never written back over an
-- old one. So there is no updated_at and no deleted_at: two columns nothing
-- writes would be state nothing consults.
CREATE TABLE history_versions (
	id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id    uuid NOT NULL,

	module       varchar(32)  NOT NULL,
	entity       varchar(64)  NOT NULL,
	record_id    uuid         NOT NULL,
	locale       varchar(35)  NOT NULL,
	version      bigint       NOT NULL,

	-- The record as this write committed it: after its validation and its
	-- normalisation, before the event that announced it. jsonb and not columns
	-- because whose fields, and which fields, belongs to the module that owns the
	-- record and changes when that module changes; the shape this table insists on
	-- is that it is an object.
	fields       jsonb        NOT NULL,
	-- The names this write moved, stored rather than re-derived: re-deriving them
	-- needs the preceding version, which retention is entitled to have removed.
	changed      jsonb        NOT NULL DEFAULT '[]',
	-- Why the write happened, in the store's own grammar ('' for an ordinary save,
	-- "restore:<n>" for a version brought back). The words a person reads come from
	-- the catalogue, never from here.
	reason       text         NOT NULL DEFAULT '',

	-- The files this version's bodies show, as the door that wrote them read them:
	-- [{"field":"body","files":["…"]}]. Nothing here needs it to describe the
	-- version — the body names its pictures — and the retention sweep does: a
	-- version it removes has to release exactly the files that version was keeping
	-- alive, and re-parsing every body at sweep time would be a second reading of
	-- the format, done at the worst moment. Stored, in other words, because a
	-- reader reads it.
	refs         jsonb        NOT NULL DEFAULT '[]',

	-- Who wrote it; NULL when nobody signed in — a seed, a job, a system write —
	-- which is an answer rather than an unknown.
	actor        uuid,
	-- The instant the value became true, in UTC: what a person reads in the panel
	-- and what the retention rule ages from.
	committed_at timestamptz  NOT NULL,

	CONSTRAINT history_versions_number   UNIQUE (tenant_id, module, entity, record_id, locale, version),
	CONSTRAINT history_versions_first    CHECK (version >= 1),
	CONSTRAINT history_versions_module   CHECK (module <> ''  AND module = lower(module)),
	CONSTRAINT history_versions_entity   CHECK (entity <> ''  AND entity = lower(entity)),
	-- A language tag keeps the spelling its tenant gave it: "pt-PT" is the tag, and
	-- lower-casing it here would break the comparison against a tenant's own
	-- languages rather than tidy it. BCP-47 tags are case-insensitive in meaning and
	-- the tenant's row is the one place they are decided.
	CONSTRAINT history_versions_locale   CHECK (locale <> ''),
	CONSTRAINT history_versions_fields   CHECK (jsonb_typeof(fields)  = 'object'),
	CONSTRAINT history_versions_changed  CHECK (jsonb_typeof(changed) = 'array'),
	CONSTRAINT history_versions_refs     CHECK (jsonb_typeof(refs)    = 'array')
);

-- The panel's read, and the restore's: one record's series, newest first. It leads
-- with tenant_id because that is what the policy filters on, and ends version DESC
-- so the newest page is a scan and not a sort.
CREATE INDEX history_versions_series ON history_versions (tenant_id, module, entity, record_id, locale, version DESC);
-- The retention sweep's candidate scan: this tenant's versions by age.
CREATE INDEX history_versions_age    ON history_versions (tenant_id, committed_at);

-- Both halves, as every module's table has them: USING is what makes another
-- tenant's versions unreadable, WITH CHECK what makes them unwritable — a record
-- id copied into another tenant's transaction is refused rather than filed.
ALTER TABLE history_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE history_versions FORCE ROW LEVEL SECURITY;
CREATE POLICY history_versions_tenant ON history_versions
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
