-- The publisher's trace context, on the row.
--
-- A trace cannot end where a process ends. A request that published an event and
-- the handler that reacted to it — in a worker, seconds or hours later, in another
-- process — were two unrelated traces, and the only way to join them was to quote
-- the event id in both. The outbox is the reason this needed a column: a
-- publisher's context cannot be carried by the process that happened to publish,
-- so the context is written where the event is written, in the transaction that
-- wrote it.
--
-- Both columns are nullable, and NULL is the ordinary case rather than a defect: a
-- periodic job has no request to inherit a trace from, and a deployment with no
-- collector configured propagates a trace it was handed but starts none of its
-- own. The relay reads them, puts them on the envelope as the two W3C
-- distributed-tracing members (kit/events/transport.Event), and the delivery's
-- span is a child of the request that caused the event.
--
-- An expand, and an unsafe one it is not: two nullable columns on a table whose
-- readers name their columns (the relay) or take the whole row by entity
-- (nothing), added without a default, so no row is rewritten and no rewrite is
-- scheduled for the contract half — the columns are the point of the file.

ALTER TABLE platformkit_outbox ADD COLUMN traceparent text;
ALTER TABLE platformkit_outbox ADD COLUMN tracestate  text;
