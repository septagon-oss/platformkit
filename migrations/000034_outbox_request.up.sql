-- Which call caused each event.
--
-- The outbox already keeps the two facts a worker cannot recover by the time it
-- relays a row: whose request it was (actor) and which trace it belonged to
-- (traceparent, tracestate — 000028). What it did not keep is which call and
-- from where. Those are readable only while the request is open, so by relay
-- time they are gone: an operator looking at a tenant's trail could see that a
-- settings write happened and by whom, and could not reach the request log that
-- explains it, nor name the address it arrived from.
--
-- Both are the request's own values, copied at the moment the event is written
-- by kit/events from kit/request — the id the caller was answered with in
-- X-Request-ID, and the peer address of the connection, never a header a client
-- could write. See kit/request's comment for why the address has one parser and
-- why it is not X-Forwarded-For.
--
-- client_ip is `inet`, not `text`, because `inet` refuses a value that is not an
-- address at insert time: a column that cannot hold a typo is one less thing a
-- reader has to distrust. NULL means there was no request — a job, a handler
-- reacting to another event, a row written before this file ran — and an absent
-- request stays absent rather than becoming the empty string, which `inet` could
-- not hold anyway.
--
-- This is an expand: nothing reads the two columns until the release after this
-- one stops writing them, so a rollback of the code loses two unread columns and
-- no data.

ALTER TABLE platformkit_outbox
	ADD COLUMN IF NOT EXISTS request_id text,
	ADD COLUMN IF NOT EXISTS client_ip  inet;

-- No index: nothing asks "every row this request wrote" of the outbox. The outbox
-- is a queue that drains and is purged; the question with an id in hand is asked
-- of the trail, and modules/audit/migrations/000025 is the index that answers it.
-- A second index nobody reads would be a write cost on the busiest table here for
-- an answer nobody asks — house rule 7, and the rule kit/db refuses a plain index
-- build with besides.
