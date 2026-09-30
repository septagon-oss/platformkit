-- The trace context of the request that caused each event.
--
-- An event is a fact about a state change, and the state change was a request.
-- Until now the outbox kept the actor — *who* — and nothing about *which call*:
-- a worker that relayed an event two seconds later could not say which request,
-- which log line and which upstream trace it belonged to. The envelope the relay
-- publishes is CloudEvents 1.0, whose distributed-tracing extension names these
-- two attributes (traceparent and tracestate), so the outbox has to hold them
-- from the moment the event was written: by relay time the request is long gone.
--
-- Both are the request's own W3C values, carried and not interpreted — see
-- kit/trace. Empty means "no request caused this", which is a periodic job, a
-- relay of a row written before this column existed, and a handler reacting to
-- another event. Nullable rather than NOT NULL DEFAULT '': the absence of a
-- trace is a fact worth storing, and an empty string and NULL would be two
-- words for it.
--
-- This is an expand: nothing reads the columns until the release after this one
-- stops writing them, so a rollback of the code loses two unread columns and no
-- data.
ALTER TABLE platformkit_outbox
	ADD COLUMN IF NOT EXISTS traceparent text,
	ADD COLUMN IF NOT EXISTS tracestate text;
