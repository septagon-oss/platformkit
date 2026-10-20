-- An erasure may remove a person's words from the trail, and nothing else may.
--
-- 000048 made the trail append-only by refusing every UPDATE in a BEFORE trigger and
-- revoking the privilege underneath it. That was right about the thing it was written
-- for — a caller rewriting what happened — and it collides with the one thing an
-- erasure must still do here: the payload column carries the person's address
-- verbatim, because an event's payload is the document the action carried, and ADR 0020
-- promises that a person's address is not findable in a tenant after their erasure. A
-- fence that refuses that write does not protect history, it hoards one person's data.
--
-- The exception is therefore the erasure's own pair of columns, and it is a capability
-- in the same shape as the expiry path's: a role may perform this UPDATE only when the
-- two columns are the only UPDATE it holds anywhere on this table — so the deployment
-- that never grants the erasure's columns has no erasure through the trail, and a role
-- that was handed a wider UPDATE gets the refusal the fence already gave — and only
-- when the row it writes changes nothing else. The second half is asked of the row
-- rather than of the caller, because `SET actor = …, payload = …` arrives with the same
-- privileges as the scrub and must still be refused.
--
-- `to_jsonb` of the two rows, minus the two columns, is the whole comparison: it costs
-- two encodes per updated row, and the only writer that ever reaches here is an
-- erasure, which is one statement per person and not a hot path. A row whose other
-- columns are equal in both encodings is the same row with its person's words taken out.
--
-- TRUNCATE keeps the unconditional refusal: this trigger is also attached to it, where
-- OLD and NEW do not exist, and the `TG_OP` guard below is what keeps that path raising
-- instead of falling through.
--
-- The body touches only pg_catalog and the row it is handed, so the pinned
-- search_path still means a caller's path resolves no other pg_attribute and no other
-- privilege function.
CREATE OR REPLACE FUNCTION audit_events_never_rewritten() RETURNS trigger
	LANGUAGE plpgsql
	SET search_path = pg_catalog
AS $$
BEGIN
	IF TG_OP = 'UPDATE'
		AND has_column_privilege(current_user, TG_RELID, 'payload', 'UPDATE')
		AND has_column_privilege(current_user, TG_RELID, 'client_ip', 'UPDATE')
		AND NOT EXISTS (
			SELECT 1 FROM pg_attribute col
			WHERE col.attrelid = TG_RELID AND col.attnum > 0 AND NOT col.attisdropped
				AND col.attname NOT IN ('payload', 'client_ip')
				AND has_column_privilege(current_user, TG_RELID, col.attnum, 'UPDATE')
		)
		AND (to_jsonb(OLD) - 'payload' - 'client_ip')
			= (to_jsonb(NEW) - 'payload' - 'client_ip') THEN
		RETURN NEW;
	END IF;
	-- insufficient_privilege is the SQLSTATE PostgreSQL answers a revoked privilege
	-- with, so whichever half catches a write, the error class is one answer.
	RAISE EXCEPTION 'audit history is append-only: %.% is never rewritten', TG_TABLE_SCHEMA, TG_TABLE_NAME
		USING ERRCODE = 'insufficient_privilege';
END
$$;

-- The capability, handed to the same grantees the trail's read is handed to (000049's
-- GRANT SELECT … TO PUBLIC is the precedent, and the application role is the only SQL
-- identity an installation serves through). It is two columns and not the table: with
-- this grant a role may still not move `actor`, `occurred_at`, `name`, `event_id`, the
-- tenant or the trace — which is what every immutability case in this module asserts,
-- and what it goes on asserting unchanged. 000048's REVOKE loop runs at an earlier
-- version, so it cannot take this back; this file is the last word, and it says less
-- than 000048 took.
GRANT UPDATE (payload, client_ip) ON TABLE audit_events TO PUBLIC;
