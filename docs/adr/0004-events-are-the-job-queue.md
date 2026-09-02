# 4. Events are the job queue

Status: accepted, 2026-09-02

## Context

The previous codebase ran background work three ways: a transactional outbox, a
`river` job queue with its own tables, workers and migrations, and ad-hoc
goroutines. The outbox and the queue solved the same problem — durable work,
retried, enqueued atomically with a state change — with two sets of tables to
migrate and two answers to "why did that not run?".

## Decision

The outbox is the job queue. A module publishes an event in the transaction
that caused it; the relay moves rows to a transport once a second; a
subscription runs the handler in a transaction scoped to the event's tenant.
Asynchronous work is what a subscriber does, and there is no second queue.

Periodic work is the one thing an outbox cannot express, because nothing
happened. That is `kit/jobs`: a schedule and an advisory lock, so exactly one
instance in the cluster runs a job per tick.

## Consequences

- Delivery is at-least-once. The relay publishes and then stamps `published_at`,
  because the other order loses events and this one repeats them. **Every
  handler must be idempotent**, keyed on `Event.ID`: stable across redeliveries
  of one event, unique across different ones.
- Enqueueing cannot fail separately from the write it belongs to: both are one
  `INSERT` in one transaction. Ordering is per stream, not per aggregate.
- Retries are the transport's: an error nacks and the event comes back, slower
  each time. A periodic job that hangs holds its lock, which is what "exactly
  one instance runs it" costs.

## Evidence

```sh
go test ./kit/events ./kit/jobs   # publish, relay, consume, redelivery,
                                 # JetStream, purge, schedules, one runner
```
