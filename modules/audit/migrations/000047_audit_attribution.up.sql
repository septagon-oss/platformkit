-- The trail says what caused an event when the cause was no person.
--
-- The outbox has carried attribution since 000046: which kind of cause wrote the
-- row, which file and line asked for it, and on whose behalf. This trail is the
-- outbox's history — modules/audit subscribes to every event every module
-- declares — and until now it copied one column out of that attribution, `actor`,
-- and dropped the rest. A published row loses its outbox twin to retention, so
-- the copy is the last copy: for a write the application made of itself the trail
-- kept the NULL `actor` that means "no person signed in" and nothing else, which
-- is the same nothing a periodic job and a relay are. The one record of "the
-- starter seed wrote this page, from seed/starter/contents.yaml" was in the row
-- this module read and did not keep.
--
-- `actor_kind` and `initiator` are copied, not derived: `actor` is who was signed
-- in and a seed run has no session, so a trail row with an actor_kind and no actor
-- says both true things at once. All four stay nullable exactly as the outbox
-- columns are, and no row already written gains a value — a write audited before
-- this release has no attribution recorded, which is a fact about the release, not
-- a missing one. Nothing is backfilled: the source line is gone by the time this
-- file runs, and a value invented here would be a citation nobody could follow.
--
-- No CHECK pair here, though the outbox has one. The trail's rows are copies of
-- rows that passed the outbox's own constraint at the moment they were written,
-- and an append-only table that the retention job alone touches is the one table
-- in this schema too big to take a validation lock for a rule its source already
-- enforces. The pair is refused where the row is made, which is where a lie about
-- a source could originate.
--
-- This is an expand: nothing had to stop writing anything for it to land, and a
-- rollback of the code loses four unread columns and no data.
ALTER TABLE audit_events
	ADD COLUMN IF NOT EXISTS actor_kind  text CHECK (actor_kind IS NULL OR actor_kind IN ('user', 'system', 'seed', 'job')),
	ADD COLUMN IF NOT EXISTS source_file text CHECK (source_file IS NULL OR (source_file <> '' AND length(source_file) <= 512)),
	ADD COLUMN IF NOT EXISTS source_line integer CHECK (source_line IS NULL OR source_line > 0),
	ADD COLUMN IF NOT EXISTS initiator   uuid CHECK (initiator IS NULL OR initiator <> '00000000-0000-0000-0000-000000000000');
