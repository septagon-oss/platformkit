# `kit/stream`: live notices on the events transport

`kit/stream` is the kernel's live door: `GET /api/v1/app/stream?topics=task.closed,work.started`,
mounted by the composition on the App surface under `httpx.SignedIn()`, answering
`text/event-stream` for as long as the caller listens.

A frame is a **notice and never a payload**:

```
retry: 3000
: pkit stream

event: resync
data: {"time":"2026-10-14T09:40:00Z","reason":"connect"}

id: 018f3c2e-2f6a-7a11-9c3d-5e7f8a9b0c1d
event: task.closed
data: {"time":"2026-10-14T09:41:00Z"}

event: dropped
data: {"topic":"task.closed","reason":"permission"}

: ping
```

That one rule is what lets the feature sit beside the events transport instead of in
front of every read path: a notice says something happened and when, and the client's
next read is the truth. Nothing in a frame is translatable, because nothing in a frame
is prose — a refusal is a problem document and reaches the translation port like every
other refusal.

## What a module declares

A module streams an event of its own, with a policy, in its manifest:

```go
Streams: []module.Stream{{
    Event:  EventClosed,                       // an event this module emits
    Policy: module.StreamAnyPrincipal(),       // or StreamPermission("task:read"),
}, {                                           // or StreamRecipient("assignee")
    Event:  EventAssigned,
    Policy: module.StreamRecipient("assignee"),
}},
```

`module.Validate` refuses a stream whose event the module does not emit, one declared
twice, one with no policy, and one whose grant no module defines — at composition,
before anything opens. `StreamRecipient` is the one policy that reads a payload: the
server reads the named member as a user id, compares it to each connection's own
principal, and no part of the payload leaves the server.

## How a notice arrives

`transport.Transport` gained `Observe(ctx, tenantID, name, fn)`: an exact-address,
non-durable, at-most-once observation. The **tenant boundary is the address** — the
filter is that tenant's one subject, and the provider still compares the address the
broker routed by against the document (`transport.AddressMismatch`) before `fn` runs,
so the tenant is decided by routing and not by what a document claims. Nothing is
claimed, nothing is retried, and the outbox stays the durable path: a notice is the
fact that something happened, not a unit of work. `kit/events/transport/observetest`
runs the same six cases against both providers.

One `Observe` per (tenant, event) per process, reference-counted by the connections
sharing it and stopped when the last one leaves. The observation belongs to the watch,
not to whichever connection opened it: the hub opens it on a context of the watch's own
life and stops it with the last departure, so a tab closing takes nobody else's notices
with it.

## What a refusal says, and where the address came from

Every refusal is a `kit/problem` document whose `detail` carries a reason code first,
the way every other refusal here does: `STREAM_ASK_INVALID` for an ask this endpoint
cannot answer as written, `STREAM_TOPIC_UNDECLARED` for a topic no composed module
declares, `STREAM_UNAVAILABLE` for this seam's own fault (a writer that cannot flush, a
broker that would not take the observation, an authority that could not answer). The
three that answer a guard's own question reuse the guard's code — `AUTH_ANONYMOUS`,
`AUTH_DENIED`, `LIMIT_EXHAUSTED` — because the refusal is the same answer arrived at
again, and a code that meant two things would be worse than no code.

The served address is `/api/v1/app/stream` where the brief said `/api/v1/stream`, and
the `app` segment is the surface saying what it is: the route is mounted on the App
surface (`httpx.Surfaces("")`, `kernel.App`) beside the access-request route the kernel
already mounts, behind a session, and an address that did not say so would be the one
App route whose address did not. Its `Cache-Control` is `no-store` rather than
the brief's `no-cache` for the same kind of reason — the App surface replaces a
storable value under a session (`kit/httpx`'s caching rule) and a stream with no end
has nothing to revalidate. Both are the brief's letter, not its intent; whoever reads
the brief should read this paragraph with it.

## The bounds, and what they count

| | Value | When hit |
| --- | --- | --- |
| topics per connection | 8 | 400 |
| connections per person per tenant, per process | 3 | 429 + `Retry-After` |
| connections per tenant, per process | `server.streams.max_per_tenant`, default 500 | 429 + `Retry-After` |
| undelivered notices per connection | 64 | the queue is dropped and **one** `resync` is sent |
| one write | 5 s, set again before every write | the connection closes |
| heartbeat | 15 s | a comment, and the session and every topic's policy re-checked |

**Both connection ceilings count one process.** A deployment behind a load balancer
with N replicas admits N times the tenant's number, and nothing here knows about the
other replicas. A cluster-wide cap is the follow-up, and until it exists this is what
the number means — say so to whoever asks for a plan feature built on it.

A revoked grant drops that topic with `event: dropped` and leaves the connection up;
an ended session closes it — at the next heartbeat, whatever the connection watches:
the session is asked again for a topic any signed-in person may watch exactly as it is
for one that names a grant, because a person who signed out is nobody's recipient. A
database that will not answer the heartbeat does neither:
the outage is logged and the next tick asks again.

## The transaction, which is never the request's

A stream outlives the query that admitted it, so the route declares
`httpx.StreamedResponse()`, and `httpx` commits the guards' transaction before the
handler is entered — the same door a route that streams its *request* already uses.
Every transaction a live connection opens after that (the connect-time policy check,
and the re-check at each heartbeat) is its own short transaction over `db.Detached`,
committed inside the call that opened it. A long-lived request would otherwise hold a
pool connection for as long as a browser kept a tab open.

`httpx.CanStream(w)` is asked before any header: the two writers this kernel puts
between a handler and the connection both answer `Flush`, so a stream served over a
proxy that buffers whole answers would believe its frames were arriving. The answer to
a writer that cannot flush is a 500 and no bytes, never a committed 200.

## The pillar contract, eight lines (decision 0064)

1. **Tenant-first.** The boundary is the address: `Observe` is given the tenant id and
   filters that tenant's one exact subject, and the provider runs
   `transport.AddressMismatch` on the routed address before the notice is handed over.
   The tenant comes from `tenancy.FromContext` and nothing on the query can name one.
2. **One process, many tenants.** No package-level state: the watches, the per-tenant
   count and the per-person count live in the `*Hub` one composition builds, so two
   compositions in one process get two hubs. The two ceilings are explicitly per
   process, and the section above says what that means.
3. **Audited.** Nothing here changes state, so the honest answer is that a seam that
   writes no row writes no audit trail: no claim is taken, no revision is checked, no
   row is inserted, and no test reads a record back that nothing wrote. What is kept
   instead is the log: every accept, refusal, dropped topic, closed connection and
   undeliverable notice is one line carrying tenant, user, topics and the request id.
4. **Traceable.** The connection's context derives from the request's, so it carries
   the span, the request id and the baggage `httpx` installs; every log line is a
   `slog.*Context` call on a context that holds them. A notice carries the publisher's
   trace context on the event and no identity beyond its own id — frames carry no
   position the server would have to honour, which is a decision, not an omission.
5. **Standard-first.** Server-Sent Events as HTML Living Standard §9.2.7 defines them
   (`retry`, comment lines, `event`/`data`/`id`, the blank-line terminator), and RFC
   9457 problem documents for every refusal. What the standard offers and this declines
   is `Last-Event-ID` replay, declined by never writing an `id` a server would owe
   rather than by redefining the format.
6. **Core stays core.** Everything is in `kit/` — the transport's `Observe`, both
   providers, `kit/module`, `kit/httpx`, `kit/app` — as declared types and one mount.
   No event name, no client, no sector and no number a customer asked for appears in
   it; a product's modules declare `Streams` and compose the route the kernel mounts.
7. **Web and mobile.** Same endpoint, same `resync` contract, and the same limit: no
   resume, because replay is unbuilt. A mobile client that reconnects is told to
   refetch rather than given a position, so it is never left holding a screen it cannot
   trust; the client itself, and the live-region flow that would exercise it, are the
   product's task named in `Named follow-ups`.
8. **Measurably better.** This repository has no `tools/pillars.py`, so there is no
   indicator to name and no number to invent. The measurements this seam moves are its
   own two budgets, and `BenchmarkFanOutTimePerNoticeForOneThousandConnections`, which
   reports `ns/connection` and `notices/s` for one publish to a thousand connections of
   one tenant, with no threshold asserted — a ceiling chosen before anything was
   measured protects nobody. The reading and its host go in the report of the round
   that runs it.

## Named follow-ups

* **Cluster-wide connection caps** — see the table above.
* **`Last-Event-ID` replay from the outbox.** The frame format writes no `id` on any
  frame but a notice, so a reconnect cannot claim a position; `resync` is what a client
  is told instead, and a client that obeys it is never left with a screen it cannot
  trust. Replay would be a new promise and needs its own design.
* **A tolerance for failed re-checks** ("how many lost heartbeats before a client is
  dropped") — a number the product should choose for its own operators.
* **The browser side, the resource shell's opt-in and the notification consumer**: the
  client task, not this one.
* **WebSocket**: not built, not stubbed, not flagged.

## Composition

**Reused**: `transport.Transport` and `transport.AddressMismatch` (the observation is
one method added where the port already lives, and the same address check the durable
delivery runs), `module.Module.Subscriptions`' neighbour slot in the manifest and
`module.Validate`'s emitted set (the stream rules are checked beside the subscription
rules, over the set they already compute), `httpx.Auth`'s constructor-only shape for
`StreamPolicy`, `httpx.Surfaces("")`/`httpx.Register` and `httpx.SignedIn()` (the
kernel's own mount, beside `mountAccessRequest`), `httpx`'s `StreamedBodyExtension`
(extended to a streamed *response*, not a second transaction door), `kit/problem` for
every refusal, `httpx.RetryAfterHeader`, `db.Detached`/`db.Run`, and the composition's
own `Options.Authenticate` and `Options.Authorize` — reached through the `Authority`
port this package declares rather than through a second session or policy reader.

**Added**: the ephemeral observation itself, because the transport had exactly one
subscription shape (durable, named, claiming) and an exact-subject deliver-new watch
exists in neither provider; `stream.Hub`, because nothing in the kernel held
per-connection state or fanned one delivery out to many readers; `stream.Registry`,
because `Validate` checks declarations and nothing collected them; `httpx.CanStream`,
because no probe of "can this response actually be streamed" existed and both wrappers
answered `Flush` whether or not the writer underneath could; the SSE frame writer,
because the format had no owner in this repository.

**Made reusable**: `observetest` — a conformance package whose cases both providers
run, the shape `modules/*/contracts/*test/` uses and the events transport never had,
ready for a third provider; `httpx.CanStream` and the `FlushError` pair on both
wrappers, which any future streaming or long-polling route needs and which now report
the truth instead of silence; `httpx.StreamedResponse()`, one extension any
response-that-waits can declare; `stream.Authority`, the port shape for "ask the guards
again from a goroutine that outlived the request"; and the per-connection queue with
its drop-and-say-so overflow, which is a pattern a file export or a long job listing
can take as it stands.
