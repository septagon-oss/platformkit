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

## Declared payloads

`transport.Declare[T](name)` pairs an event name with the Go type of its payload, and
a module's manifest carries the pairs as `module.Module`'s `Payloads`. Nothing on the
delivery path reads them: `Publish` still takes any value, the outbox stores what it was
given, and a module that declares none validates, boots and publishes exactly as it did.
What they feed is the description and the AsyncAPI and Backstage documents projected from
it — which is how a document can say that `task.assigned` carries `taskId`, `assigneeId`,
`status` and `at` instead of "some JSON". A declared type is projected with
`entity.JSONSchema(entity.FieldsOf(t))`, the same projection an entity gets, so a payload
struct is written with `json` tags and nothing else.

The type has to be the one `Publish` is given for that name, and that rule is the
module's: no code can see a call site it cannot reach. `module.Validate` refuses a payload
for an event the module does not emit and refuses one name twice, and the composition's
committed description shows a missing or wrong entry as a diff, but neither reads the
publisher. So declare the payloads in `contracts/events.go`, where the names and the
structs already are, and change the pair in the commit that changes either — a document
that describes the wrong record misleads a generated client, which is worse than the
nothing an undeclared event gets. A module's own contracts test is where the pair can be
asserted, and no module's does today: until one does, the pairing between a `Declare` and
a `Publish` is a reviewer's.

## Wire format

Every event a transport carries is a [CloudEvents 1.0](https://github.com/cloudevents/spec/blob/v1.0.2/cloudevents/spec.md)
envelope in structured content mode, so a broker bridge, an event router or an
AsyncAPI document can read a PlatformKit event without importing this package.
`Event` stays the Go programming model — the outbox stores columns, not
envelopes — so only the JSON representation changed.

| Member | Value |
| --- | --- |
| `specversion` | `"1.0"` |
| `id` | `Event.ID` |
| `source` | `"/" + module`: the first dot-separated segment of `Event.Name` (`task.assigned` → `/task`); a name with no dot uses the whole name |
| `type` | `Event.Name` |
| `time` | `Event.At` in RFC 3339 with nanoseconds, UTC |
| `datacontenttype` | `"application/json"` |
| `data` | `Event.Payload`, omitted when empty |
| `tenantid` | `Event.TenantID`, an extension attribute, required |
| `actor` | `Event.Actor`, an extension attribute, omitted when it is the nil UUID |

Extension attribute names are lower-case, as the specification requires. Decoding
checks `source` against `type` rather than trusting either, and still decodes the
previous shape (`id`, `name`, `tenantId`, `payload`, `at`, `actor`), which it never
produces.

**Roll out worker roles before web roles, and do not run a previous worker against a
newer web role after that.** The previous shape still decodes, so a worker on this
release reads what an old web role publishes, and the subject, stream and durable
names are unchanged: that is the window a rolling restart lives in. The reverse is
not safe. `id`, `tenantid` and `actor` differ from the old member names only in
case, and JSON member names are matched case-insensitively, so a previous worker
reading an envelope takes its tenant and its id but finds no `name`, no `payload`
and no `at` — a handler that ignores its payload then claims and acknowledges an
event whose contents it never saw. `kit/events/transport` pins both shapes in
`testdata/`.

Run `go test -race ./kit/events/transport ./kit/events/providers/...` for portable
envelope, memory delivery and local NATS TLS/credential checks. The SQL/broker
recovery tests remain in `kit/events`; run them with the development PostgreSQL
and NATS settings described in [Contributing](../../CONTRIBUTING.md#verify-at-the-relevant-boundary).
Reserve the broker for that suite: its stream-drift test changes and reconciles
shared stream settings. These checks do not qualify a deployed broker or a
downstream application's handler idempotency.
