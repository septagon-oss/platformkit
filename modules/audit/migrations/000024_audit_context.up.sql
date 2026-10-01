-- The three questions an audit row could not answer.
--
-- The trail answers who (actor), what (name, payload) and when (occurred_at).
-- Asked "from where", or "which call", or "show me everything this trace
-- touched", it had nothing: the trail is written by an event handler, in a
-- transaction the relay opened, after the request that caused the event was
-- over — and the three facts live only while a request is open. So they are
-- carried here on the event itself, by kit/events from kit/request, through
-- migrations/000030, and this file is where the trail keeps them.
--
-- Nothing is backfilled, and that is the honest boundary rather than a gap in
-- the work: a request id, an address and a trace that were never captured cannot
-- be reconstructed, and inventing them for old rows would be writing history a
-- second time in a table whose whole shape exists to refuse that. A NULL in
-- these three columns means "recorded before the trail could answer this", and
-- it says so the same way the unchained rows of 000026 do.
--
-- traceparent is stored verbatim, as W3C Trace Context spells it
-- (`00-<32 hex>-<16 hex>-<flags>`), and the trace *id* is not given a column of
-- its own: one string in two columns is two strings that can disagree. The
-- expression index in 000025 is what makes "every row this trace touched" an
-- index lookup rather than a scan.
--
-- client_ip is `inet` for the reason migrations/000030 gives: the type refuses a
-- value that is not an address at insert time. No X-Forwarded-For — a header a
-- client can write is not an address (kit/httpx's ClientAddr is the one parser,
-- and modules/auth's session list has always recorded the same fact).
--
-- The index is the next file, not this one: this table already exists, and
-- building an index on it without CONCURRENTLY stops every writer for the length
-- of the build, which is the rule kit/db/migration_rules.go refuses.

ALTER TABLE audit_events
	ADD COLUMN IF NOT EXISTS request_id  text,
	ADD COLUMN IF NOT EXISTS client_ip   inet,
	ADD COLUMN IF NOT EXISTS traceparent text;
