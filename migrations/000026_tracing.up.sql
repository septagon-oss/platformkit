-- The trace context of the work that published an event, on the row the way
-- 000009 carries its actor.
--
-- It is a pair of columns and not a key inside the payload for the same reason
-- the actor is: it is the same question about every event whatever its module,
-- and writing it into the payload would make every publisher's payload type
-- carry a field none of them asked for and make a handler read delivery state
-- out of its own domain object. The relay reads both back and puts them on the
-- envelope as the CloudEvents traceparent and tracestate extension attributes,
-- which is how a worker's handler span becomes a child of the request that
-- caused the event: the outbox is the hand-off, so the context is stored with
-- the event and not carried by the process that happened to publish it.
--
-- Both nullable, and NULL is the common case rather than the broken one: a
-- periodic job, the relay, and a request in a deployment that traces nothing
-- have no context to carry, and such a delivery starts a trace of its own.
ALTER TABLE platformkit_outbox ADD COLUMN traceparent text;
ALTER TABLE platformkit_outbox ADD COLUMN tracestate text;
