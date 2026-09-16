-- The ids an audit row is about, so a record's trail is an index lookup.
--
-- modules/audit answers "what happened to this row" by looking for the row's id
-- anywhere in the payload, at any depth, because a payload names its row
-- differently depending on who published it: a generated write carries the row,
-- so the id is "id", while a command carries its own argument, where it is
-- "taskId" or "contentId". That search is exact and it is also a sequential
-- scan — measured at 75 ms over 200,000 rows, growing with the table, paid on
-- every read because the page carries a total. No jsonb index serves it: a GIN
-- index on the payload cannot answer `$.** ? (@ == …)`, which was tried before
-- this column was written.
--
-- So the ids are lifted out of the payload once, when the row is recorded, into
-- an array an index can answer from. The semantics are the ones the search had:
-- every uuid the payload mentions, wherever it sits.
ALTER TABLE audit_events ADD COLUMN records uuid[] NOT NULL DEFAULT '{}';

-- The rows already written, read the same way the query used to read them. This
-- rewrites the table once, which is why it is here and not in the read path.
UPDATE audit_events SET records = COALESCE((
	SELECT array_agg(DISTINCT (value #>> '{}')::uuid)
	FROM jsonb_path_query(payload, '$.**') AS value
	WHERE jsonb_typeof(value) = 'string'
	  AND (value #>> '{}') ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
), '{}')
WHERE payload IS NOT NULL;

-- jsonb_path_ops is not what this needs: the question is "does this array
-- contain that id", which is what the default array operator class answers.
CREATE INDEX audit_events_records ON audit_events USING gin (records);
