# Events and delivery providers

`kit/events` owns transactional publication, SQL outbox relay, tenant-scoped
handling claims and terminal records. A broker acceptance alone does not prove
that a handler committed. The existing `Publish`, `Consume`, `Relay`, `Purge`,
`Subscription` and `Handler` APIs retain those responsibilities.

For an application that owns its own persistence, import the parts directly:

| Import beneath `github.com/septagon-oss/platformkit/` | Responsibility |
| --- | --- |
| [`kit/events/transport`](transport/) | `Event`, `Transport`, `Sink`, `ValidName`; only standard library and UUID dependencies |
| [`kit/events/providers/memory`](providers/memory/) | `New()` delivers within one process and waits for handling or terminal recording |
| [`kit/events/providers/nats`](providers/nats/) | `JetStream(url, options...)` and `Connect(config.NATS)` select the existing NATS transport |

`events.Event`, `events.Transport` and `events.Sink` are aliases; `ValidName`
forwards to the shared grammar. `events.Memory()` is gone: call `memory.New()`,
so the SQL outbox package imports none of its own providers and the package gate
holds it there. Existing outbox consumers can migrate those imports independently. The retry
ladder, handler-attempt cap and seven-day retention have one internal owner;
moving packages does not change stored subjects, stream names or durables.

The NATS constructors have moved out of the SQL package. Replace
`events.JetStream(...)` with `nats.JetStream(...)` and
`events.ConnectJetStream(settings)` with `nats.Connect(settings)`, importing
`kit/events/providers/nats` under an unambiguous alias when also using the SDK.
The root constructors are removed so SQL outbox consumers no longer import the
NATS SDK. The reference application's transport selection uses the new owner.
Removing these constructors breaks stable v1 source APIs. This change belongs to
the planned `/v2` release in [RELEASE](../../RELEASE.md), and must not ship as a
compatible v1 minor or patch release.

`kit/app` imports neither provider: the composing application passes both
constructors as `app.Transports{Memory: memory.New, JetStream: nats.Connect}`
and the kernel selects one by `nats.transport` and the role, refusing at `New`
when the selected name has no constructor.

`Connect` validates the existing `config.NATS` settings and then connects;
`JetStream` accepts official NATS options. Both can create the existing
`PLATFORMKIT` stream on first connection. `Subscribe` reconciles stream settings
and durable consumers. These are broker operations, not read-only readiness
checks. The caller drains work and closes the returned `io.Closer` connection.
No new configuration namespace, broker or lifecycle service is introduced.

Delivery is at least once. Independent sinks must provide their own durable
idempotency and tenant checks; the transport cannot supply database isolation.
Consumer reconciliation may replay events when an incompatible durable is
recreated. Memory has no restart persistence and does not coordinate duplicate
durables across processes. When using the SQL outbox, handling and terminal
claims commit atomically, and unfinished memory deliveries leave rows pending.
External effects still require provider idempotency.

Run `go test -race ./kit/events/transport ./kit/events/providers/...` for portable
envelope, memory delivery and local NATS TLS/credential checks. The SQL/broker
recovery tests remain in `kit/events`; run them with the development PostgreSQL
and NATS settings described in [Contributing](../../CONTRIBUTING.md#verify-at-the-relevant-boundary).
Reserve the broker for that suite: its stream-drift test changes and reconciles
shared stream settings. These checks do not qualify a deployed broker or a
downstream application's handler idempotency.
