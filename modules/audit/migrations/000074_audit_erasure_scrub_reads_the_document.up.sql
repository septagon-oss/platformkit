-- An erasure may take a person's words out of a trail document. It may not put a fact in.
--
-- 000055 opened `payload` and `client_ip` to the erasure and fenced the write by which
-- columns moved. A column list is half the question: it refuses `SET actor = …, payload = …`
-- and it accepts any statement that stayed inside those two columns — including the one that
-- matters, `SET payload = '{"email":"somebody.else@…","active":false}'`, which keeps the
-- actor, the moment and the event name and replaces what the row says happened. The document
-- is the fact: an event's payload is the document the action carried, so the fence is now
-- asked about the document, and the one change it admits is the old document with spans
-- replaced by an erasure token.
--
-- The shape is checked by cutting the new document at every token and asking for the pieces
-- that are left to sit inside the old one, each after the piece before it. A rewrite that
-- invented something — another person's address, a flag turned over — leaves a piece the old
-- document cannot supply, and no arrangement of tokens hides it, because a token is the only
-- text a rewrite may add. The check costs one encode and one split per updated row, and the
-- only writer that ever reaches here is an erasure: one statement per person, not a hot path.
--
-- What that leaves, stated rather than implied: a role holding 000055's two columns may still
-- take text *out* of a document and leave a token where it was, because "which span was
-- removed" is not recoverable from the row alone and no rule about the two documents can tell
-- an erasure's removal from a removal nobody asked for. It may not put text in, move a fact,
-- or set a client address to something other than nothing. The capability is what the
-- deployment granted when it handed a role those two columns, and README.md's Limits carries
-- the residue beside it.
--
-- `client_ip` gets the one rule it needs: it may go NULL, which is what an erasure does with
-- an address, or stay exactly as it was. 000055 left it out of the column comparison it did
-- have, so this is where it stops being the exception.
--
-- The privilege half of 000055 stays for the reason it was written: this caller must hold
-- these two columns and no other column of this table, so a deployment that never grants the
-- two has no erasure through the trail at all, and a role handed a wider UPDATE gets the
-- refusal the fence already gave. The grant itself lives in that file and is not repeated
-- here; this file changes only what the trigger asks of a row that gets through it.
--
-- TRUNCATE keeps the unconditional refusal: this trigger is attached to it too, where OLD and
-- NEW do not exist, and the `TG_OP` guard below is what keeps that path raising instead of
-- falling through. The body touches only pg_catalog and the row it is handed, so the pinned
-- search_path still means a caller's path resolves no other pg_attribute and no other
-- privilege function.
CREATE OR REPLACE FUNCTION audit_events_never_rewritten() RETURNS trigger
	LANGUAGE plpgsql
	SET search_path = pg_catalog
AS $$
DECLARE
	-- The mark personaldata.Subject.Token() leaves in free text where an identifier was:
	-- "<erased " and the first eight characters of the person's id. It is spelled as a
	-- pattern and not as a constant because the fence has to recognise any person's mark in
	-- any document, and one deployment's person is not another's.
	mark CONSTANT text := '<erased [0-9a-f]{8}>';
	pieces text[];
	piece  text;
	rest   text;
	pos    integer := 1;
	found  integer;
	erased boolean := true;
BEGIN
	IF TG_OP = 'UPDATE' THEN
		IF NEW.payload::text = OLD.payload::text THEN
			erased := true;
		ELSIF NEW.payload::text !~ mark THEN
			-- A document that changed and carries no mark did not change the way an
			-- erasure changes one.
			erased := false;
		ELSE
			FOREACH piece IN ARRAY regexp_split_to_array(NEW.payload::text, mark) LOOP
				-- pos counts from the start of the old document, so the rest of it is cut
				-- from the old document and not from the rest that was cut last time.
				rest := substr(OLD.payload::text, pos);
				found := strpos(rest, piece);
				IF found = 0 THEN
					erased := false;
					EXIT;
				END IF;
				pos := pos + found + length(piece) - 1;
			END LOOP;
		END IF;
	END IF;
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
			= (to_jsonb(NEW) - 'payload' - 'client_ip')
		AND (NEW.client_ip IS NULL OR NEW.client_ip = OLD.client_ip)
		AND erased THEN
		RETURN NEW;
	END IF;
	-- insufficient_privilege is the SQLSTATE PostgreSQL answers a revoked privilege
	-- with, so whichever half catches a write, the error class is one answer.
	RAISE EXCEPTION 'audit history is append-only: %.% is never rewritten', TG_TABLE_SCHEMA, TG_TABLE_NAME
		USING ERRCODE = 'insufficient_privilege';
END
$$;
