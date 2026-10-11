-- Who, or what, caused each event, when the cause was not a person.
--
-- The outbox has carried `actor` since 000009: the uuid of the person whose
-- request this was, NULL when nobody's was. NULL answers "no person" and nothing
-- else, so a write made by the application itself — a seed run applying a file,
-- later a job that reconciles — arrives as the same nothing as a relay with no
-- request behind it, and the one fact that distinguishes them (which file, which
-- line, on whose behalf) had nowhere to go. Audit subscribes to these events
-- asynchronously: what its row can report is bounded by what the durable event
-- carried, so a source left out here cannot be recovered downstream.
--
-- `actor_kind` names the sort of cause ('seed' today; 'user' and 'system' are
-- the other words this kernel already uses, see kit/tenancy.PolicyActorKind) and
-- `initiator` names the person on whose behalf it ran, which is why the CHECK
-- below refuses the pair that would lie: a kind with an initiator it did not
-- name in `actor`... and no kind with one it did. `initiator` is deliberately
-- separate from `actor`: `actor` is who *signed in*, and a seed run has no
-- session, so it stays NULL rather than carrying a fabricated login. The audit
-- label for such a row is the kind, and the person who ran it is here.
--
-- The four are nullable and no existing row gains a value: an event written
-- before this release has no attribution, which is a fact, not a missing one.
-- This is an expand — nothing had to stop writing anything for it to land.
ALTER TABLE platformkit_outbox
	ADD COLUMN IF NOT EXISTS actor_kind  text CHECK (actor_kind IS NULL OR actor_kind IN ('user', 'system', 'seed', 'job')),
	ADD COLUMN IF NOT EXISTS source_file text CHECK (source_file IS NULL OR (source_file <> '' AND length(source_file) <= 512)),
	ADD COLUMN IF NOT EXISTS source_line integer CHECK (source_line IS NULL OR source_line > 0),
	ADD COLUMN IF NOT EXISTS initiator   uuid CHECK (initiator IS NULL OR initiator <> '00000000-0000-0000-0000-000000000000');

-- A source is a place, so it is named in whole or not at all: a line with no
-- file, or a file with no line, is a citation nothing could follow.
ALTER TABLE platformkit_outbox
	ADD CONSTRAINT platformkit_outbox_source_pair
	CHECK (source_file IS NULL = (source_line IS NULL));
