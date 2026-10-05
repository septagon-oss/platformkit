-- Translation: any field of any record can be said in another language.
--
-- One table holds every translation in the installation, of every module, in
-- every locale. The entity is modules/translation/contracts/translation.go and
-- the columns below are its fields. There is no row per record and no column
-- per locale: the unit is one field of one record in one language, because that
-- is the thing that is translated, the thing that goes stale on its own, and
-- the thing one person reviews while another has not looked at the title yet.
--
-- A translation is not a record. It has no slug, no lifecycle and no list of
-- its own; it is a field of a record read in another language. So the identity
-- below is the whole model, and everything a translator sees — missing,
-- complete, outdated, machine — is derived from these rows plus the source.

CREATE TABLE translations (
	id            uuid        NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
	-- The column row-level security matches on; migrations/000001 explains it.
	tenant_id     uuid        NOT NULL,
	created_at    timestamptz NOT NULL DEFAULT now(),
	updated_at    timestamptz NOT NULL DEFAULT now(),

	-- The two names of the mounted Spec that declares the field: its module and
	-- its entity. They are the Spec's own, so a translation can never be
	-- attached to a resource that stopped declaring it — rest.Spec.check refuses
	-- the mount that would orphan one.
	module        text        NOT NULL,
	entity        text        NOT NULL,
	-- The source row's id. A foreign key in meaning and not in SQL: the row it
	-- points at lives in another module's schema and belongs to another module's
	-- migration, and a cross-schema FK would make this file that module's
	-- dependency. The module's delete command holds it instead, and C16 is the
	-- test that says so.
	record_id     uuid        NOT NULL,
	-- The JSON field name, which is the only name a caller ever uses: "body".
	field         text        NOT NULL,
	-- The tenant's own declared tag — "pt-PT", not a folded "pt" — because the
	-- tag a response answers with is the tag the tenant wrote down.
	locale        text        NOT NULL,
	-- The translation itself.
	value         text        NOT NULL,
	-- The source this translation was made from, kept as the translator saw it,
	-- and its digest. Both are what the stale highlight compares against, and
	-- the copy is what makes the highlight survive a source that has since
	-- moved twice: re-basing it onto the current source would delete the
	-- paragraph the reviewer was working on. source_hash is always the digest of
	-- source_text, never of value — one test pins that for every row.
	source_text   text        NOT NULL,
	source_hash   char(64)    NOT NULL,
	-- complete, outdated or machine. `missing` is deliberately absent: it is the
	-- state of a (record, field, locale) pair with *no* row, and a row created
	-- empty to hold it would be a row whose only purpose is to stand in for its
	-- own absence.
	status        text        NOT NULL,
	-- Who wrote it. Provenance is never erased: a person who types over a
	-- machine draft keeps origin 'machine' and gains a reviewed_at.
	origin        text        NOT NULL,
	-- When a person accepted this text. Nothing machine-made is public while it
	-- is NULL, which is the one column the public read keys on.
	reviewed_at   timestamptz,
	-- The principal of the last write. A person's own text is reviewed by being
	-- typed, so a human row needs no review to have one.
	translator_id uuid,
	-- Per-row optimistic concurrency: two translators in one locale, one loser,
	-- and no silent overwrite of the winner.
	revision      bigint      NOT NULL DEFAULT 1,

	CONSTRAINT translations_identity UNIQUE (tenant_id, module, entity, record_id, field, locale),
	CONSTRAINT translations_field    CHECK (field <> '' AND field = btrim(field)),
	CONSTRAINT translations_locale   CHECK (locale ~ '^[A-Za-z]{2,3}(-[A-Za-z0-9]{1,8})*$'),
	CONSTRAINT translations_value    CHECK (value <> '' AND btrim(value) <> ''),
	CONSTRAINT translations_status   CHECK (status IN ('complete','outdated','machine')),
	CONSTRAINT translations_origin   CHECK (origin IN ('human','machine')),
	-- A review stamp means a person accepted a machine draft. A human row has no
	-- review, because what a person typed is reviewed by being typed: the column
	-- would be a second, contradictable account of a fact origin already states.
	CONSTRAINT translations_reviewed CHECK (reviewed_at IS NULL OR origin = 'machine'),
	CONSTRAINT translations_revision CHECK (revision >= 1),
	CONSTRAINT translations_hash     CHECK (char_length(source_hash) = 64)
);

-- The identity index above already answers every per-record question the read
-- path asks, because it leads with tenant, module, entity and record. This is
-- the one question it cannot: the overview and the overlay addressing one
-- entity's rows *locale-first* for a page of records. Nothing is indexed on
-- status (the overview counts missing, outdated and complete in one pass and
-- would never use it) or on the three timestamps (read back on the record,
-- never filtered on).
CREATE INDEX translations_per_locale ON translations (tenant_id, module, entity, locale, record_id);

-- The shape every tenant-owned table has; migrations/000001 explains it.
ALTER TABLE translations ENABLE ROW LEVEL SECURITY;
ALTER TABLE translations FORCE ROW LEVEL SECURITY;

CREATE POLICY translations_tenant ON translations
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
