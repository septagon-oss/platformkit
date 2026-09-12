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
  because the other order loses events and this one repeats them. Memory waits
  for committed handling or terminal recording; JetStream waits for durable
  broker acceptance. A short relay deadline leaves incomplete local work
  unstamped while its subscription continues on the worker context. Memory retry
  counts are process-local until terminal recording commits; a restart before
  that commit can retry the handler. JetStream retains its broker delivery count.
- Committed database handling is deduplicated while its completion record is
  retained. `Consume` claims `(Event.ID, durable)` in `platformkit_handled`
  inside the handler's transaction; redelivery skips an existing claim or dead
  letter. Failure rolls the claim back with the work so it can retry. The key
  includes the subscription because two interested modules are two pieces of
  work. Old marks remain while their outbox row or terminal failure record
  exists. An external effect accepted before a failed database commit still
  needs provider idempotency.
- Enqueueing cannot fail separately from the write it belongs to: both are one
  `INSERT` in one transaction. Ordering is per stream, not per aggregate.
- A durable consumer is shared by every worker replica, through a JetStream
  deliver group named after the durable. A push consumer with no group belongs
  to one subscriber and refuses the next, so the second worker crashlooped on
  its first subscription while the boot log recommended running one — the
  release review found it. A group is one delivery shared between the members
  rather than one per member, so each event is still handled once, and it is a
  smaller change than pull consumers: a queue name at the subscribe and a field
  in the reconciliation.
- The stream is reconciled on every subscribe, the way consumers are, because it
  outlives every process that connects and its settings are otherwise a copy of
  the code that nothing keeps in step. Subjects and max age are updated;
  retention and storage cannot be changed on a live stream, and a stream — unlike
  a consumer — cannot be recreated without throwing away the messages in it, so
  those two refuse the boot instead.
- Handler attempts are bounded by `maxDeliveries`. Terminal recording atomically
  claims completion and writes `platformkit_dead_letters`; a failed write rolls
  both back. Broker redeliveries beyond the cap retry only terminal recording,
  with backoff, and terminate after it commits. Broker delivery counts include
  attempts that crash or wait before a handler starts. After a restart the last
  handler error may be unavailable; the record states that limitation.
  Dead letters and their claims remain until explicit operator review; relaying
  alone does not replay them. The stream still has its configured seven-day
  retention; terminal retry does not establish unbounded broker retention.
- The relay takes no advisory lock, because `FOR UPDATE SKIP LOCKED` is already
  the concurrency control and a lock would only make one blocked relay stop
  every replica's relay. Every other periodic job does take one; `jobs.Job` says
  which with `Parallel`. A periodic job that hangs holds its lock, which is what
  "exactly one instance runs it" costs — so the relay, the one job that can
  block on a network, is the one job that does not hold one.
- One tenant's failure inside `jobs.PerTenant` does not stop the others, and
  neither does one row's: the helper hands over the tenant on a context and the
  connection, so the job opens a transaction per row, and the errors are joined
  so it still reports as failed. Partial progress is the intended outcome.

`Sink.Dead` returns an error so transports can retain failed terminal recording.
Custom transport adapters must forward that error; adopting this source API
change includes updating their wrappers and testing recovery.

## Evidence

```sh
go test ./kit/events ./kit/jobs   # publish, relay, consume, exactly-once,
                                 # redelivery, dead letters, concurrent relays,
                                 # JetStream, purge, schedules, one runner
```
