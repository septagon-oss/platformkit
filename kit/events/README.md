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
ladder, handler-attempt cap and seven-day retention have one internal owner.

## The envelope, the subject and what a rollout has to expect

The body on the broker is **CloudEvents 1.0 in structured content mode**:
`specversion`, `id`, `source` (`/<module>`), `type` (the event name), `subject`,
`time`, `datacontenttype`, `data`, plus `tenantid` as a **required** extension
where the specification leaves extensions optional — an event with no tenant has
no transaction to deliver it in — `app` as the slug of the app whose process
published it, absent for a deployment that names none, `traceparent`/`tracestate`
when a request caused the event, and `baggage` when that request had an id to leave
behind. `Event` stays
the programming model and the outbox keeps
storing columns; `MarshalJSON`/`UnmarshalJSON` in
[`transport/cloudevents.go`](transport/cloudevents.go) own the wire form.

The subject is `platformkit.<tenant>.<module>.<event>`
([`transport/subject.go`](transport/subject.go)), so a tenant's backlog is an
address and a durable can be per tenant (decision 0053 §1). A composition that names
its app publishes at `platformkit.<app>.<tenant>.<module>.<event>` and filters
`platformkit.<app>.*.<module>.<event>` first
(`appname.Subject`, `appname.Filters`) — one segment, spelled by the package that
owns the name, and no envelope member the relay has to invent: the address and the
`app` extension are two spellings of the same fact, and `UnmarshalJSON` refuses a
document whose `subject` disagrees with the `(app, tenantid, type)` it carries.
Subscriptions keep
one durable per (module, event) and filter `platformkit.*.<module>.<event>`, plus
`platformkit.<module>.<event>` while the rollout window below is open
(`transport.Filters`); an operator who wants one tenant's queue filters that
tenant's exact subject. The stream is still `PLATFORMKIT` with `platformkit.>`.

Four consequences, each a test rather than an assurance:

* **The pre-envelope shape decodes and is never written**, and a subscription
  answers it: the previous build published at `platformkit.<module>.<event>`,
  three tokens, while this build's filter carries a tenant token that message
  does not have, so a decoder nothing subscribes to is not a window. Deploy the
  consumers first — publishers moved first write an address no old consumer's
  filter can match, and only `DeliverAll` on the recreated consumer picks those
  rows back up, within the week the stream keeps. When the last previous-build
  publisher is gone the second filter goes, the consumer is made again, and the
  window shuts. A pre-envelope document with no tenant or no event name is
  refused in either form.
* **A delivery whose document does not claim the address it arrived at is
  terminated** by the provider that routed it, before any handler's transaction
  opens (`transport.AddressMismatch`). A consumer's filter spells out the module
  and the event and leaves the tenant a wildcard, and `Consume` opens its
  transaction in the tenant the envelope names, so without this comparison a
  message stored on one tenant's address and stamped as another's would be
  handled inside the second tenant's rows on the strength of its body — a
  boundary held up by convention, with the broker credential as its key. The
  envelope's own `subject == Subject(tenantid, type)` proves only that the
  document agrees with itself, which is what a self-consistent forgery satisfies.
  `platformkit.<module>.<event>`, the previous build's address, names no tenant
  and so contradicts nothing: refusing it would be refusing the bullet above.
  Pinned by
  `TestAMessageStoredOnOneTenantsAddressIsNotDeliveredInsideAnotherTenantsTransaction`
  and by `TestADeliveryIsCheckedAgainstTheAddressItArrivedAt`.
* **Every stored consumer is deleted and made again**, because it went from one
  `filter_subject` to a `filter_subjects` set and NATS cannot change one in
  place. It asks for
  `DeliverAll`, so that is a re-delivery of the stream — and every replay is
  claimed in `platformkit_handled` before the handler runs, which is why the
  reconciliation `reconcile` logs a line rather than an incident. Independent
  sinks that are not `Consume` must supply the same durable idempotency.
* **The trace context is stored with the row** (`000028_outbox_trace.up.sql` and
  `000041_outbox_baggage.up.sql`, all nullable) and carried onto the envelope by the
  relay, because by relay time the request is gone. `kit/trace` fixes the W3C format and
  collects nothing: it holds no span, no exporter and no sampling decision.
  `kit/telemetry` names the vocabulary of a span, and this package opens three of them —
  one relay pass, one publication per row and one delivery, the last two parented from the
  context stored on the row and each naming the tenant of the row it published — so the
  trace does continue into the handler. The provider and its exporter are installed by
  `kit/app` alone: a composition that configures no endpoint propagates a context and
  exports nothing. See [kit/telemetry](../telemetry/README.md).

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

A module's manifest declares each event with the Go type of its payload
(`events.Declare[contracts.Invited](contracts.EventInvited)`); the outbox refuses
a payload that is not a projection of that type ([`schema.go`](schema.go)) inside
the publisher's own transaction — a member the projection cannot describe
constrains nothing, the same honest unknown the rendered schema answers with
`true`. The declaration belongs to the app that composed the module:
`events.DeclareApp(slug, list)` keeps one contract per app, and a publish is
checked against the declaration of the app that holds the tenant its row belongs
to (`tenants.app`, the same column `RelayApp` claims rows by and `Consume`
routes deliveries by). A second app composed into the same process adds its own
list and removes nobody else's — otherwise booting academy would take acme's
contract out of the door acme's events pass through
([`catalog.go`](catalog.go)) — and `kit/app.AsyncAPI` renders the composition's
catalogue from the same declaration
(`apps/platformkit/testdata/asyncapi.json`) — as the message's `payload`, which
is where AsyncAPI says a reader will look, and not wrapped in a member of its
own. `events.Replay` is the operator's verb for a dead letter: it clears the
claim and the terminal record, returns the row to pending, and records
`platformkit.event_replayed` with the operator's actor and their stated reason.
Both are required of the caller: a replay that cannot name who ordered it is
refused before the transaction opens, and it writes nothing and emits nothing. A
deployment that names itself asks `ReplayForApp`, which puts one more question to
the locked row's tenant — which app holds you (`tenants.app`) — and refuses one
that answers anything else: an id is not a boundary, and an installation that
cleared the claims of an event it will never deliver would be one app removing the
record that another app's handler finished. The refusal is made inside the same
transaction, before the first delete, so it leaves the claims, the dead letters and
the publication stamp where they were and emits nothing.
`Purge` leaves an outbox row that a dead letter still describes, because that row
is the payload's only copy and a replay of it has to be reachable; clearing the
dead letter is what lets the history window take the row.

`events.MoveLedger(ctx, conn, app, requestedBy)` is the drain for an installation that
began as a deployment of one app and later set `nats.app`. `appname.Durable` puts the
app in front of every consumer name, and `platformkit_handled` and `platformkit_dead_letters`
key their rows by that name, so from the boot that sets the slug onward every claim the
deployment ever earned sits on the far side of the rename: the same event, handled once
already, answered again as a first delivery. The drain is one transaction under its own
system capability: a lock on each durable whose rows it is about to rename — a claim
written at an unscoped durable takes the matching shared advisory lock in the schema
itself (migrations/000044), and the move asks for the exclusive one and refuses rather
than waiting — and a lock over each of the app's tenants, taken before a row is read.
The tenant's key is not reach beyond the harm: the durables to lock are discovered from
committed ledger rows, so the first claim of a subscription, which has none, names nothing
for the durable list to catch, and a move that found nothing to rename would answer zero
rows moved as a success while that handler still held its claim, boot would open the app's
scoped consumers over it, and the re-publish that follows would run a handler that had
already finished. The claim takes its two keys in the order the move asks them — its
tenant's first, then its durable's — and the order is what makes the tenant key worth
having: a claim that reached for its durable first can be queued behind another move's
durable lock with its own tenant still unnamed, and under that claim a placement can
commit and this app's own move answer zero, which is the same double handling arriving by
the door the locks were cut to close. Two shared locks per unscoped claim, and a moved
deployment pays neither. A table lock is what this replaced, and it was wrong: two apps share
these tables, so one app's ordinary traffic refused the other app's move on every boot
and every job tick, and the window the move exists to close stayed open while both
apps' consumers ran. The move then takes the claims of the tenants this app holds — a
claim belongs to the tenant it was made in and a tenant belongs to one app
(`tenants.app`) — so a copy of each unscoped ledger onto the app's prefix carries
`handled_at`, `name`, `error` and `failed_at` verbatim, the purge ages on them, and the
delete that follows removes exactly the rows that copy read; one `platformkit.ledger_moved`
record per tenant whose claims moved goes into the same commit. The claims of a tenant
whose `tenants.app` is empty belong to whichever deployment runs app-less beside this
one and are left where its consumer looks for them. A delivery mid-claim therefore
holds the durable and the move refuses, naming itself, having written and emitted
nothing; and a delivery that names no app re-reads `tenants.app` inside the transaction
that writes its claim, because a tenant that took an app while the delivery was being
read is the one fact about it the placement changes underneath it. Such a delivery writes
no claim and refuses its copy as the check before the transaction does: a mark under the
unscoped durable would be a mark in the one ledger that tenant's own app will never look
in, which is the window the move exists to close, marked instead of closed. `kit/app` runs
the step at boot and opens no consumer until it answers, so a refusal waits — the
worker that consumed over an unscoped ledger would replay events whose claims are
committed under the old name, which is the harm the move exists to close — and the
`ledger-move` job runs the step again on the scheduler's lock. An app that names
nothing has no prefix to move onto, so for it the step answers with the zero report and
kit/app neither schedules the job nor declares the event — see [`ledger.go`](ledger.go)
for the reasoning and the cases beside it. `kit/db`'s `phase=data` window refuses the
same act against a table keyed by anything other than the tenant, which is why this is a
drain its owner owns in a job rather than a migration.

## Limits

Delivery is at least once. Independent sinks must provide their own durable
idempotency and tenant checks; the transport cannot supply database isolation.
That sentence is about a reader that is not this provider — a bridge or a foreign
consumer reads the subject as untyped text and decides its own tenancy from it.
The one thing the provider does own is its own deliveries: the address it routed
by has to be the address the event names, because that is how the tenant of
`Consume`'s transaction is settled rather than guessed from a body. Neither is a
substitute for row-level security, which is where the rows are actually scoped.
Memory has no restart persistence and does not coordinate duplicate
durables across processes. When using the SQL outbox, handling and terminal
claims commit atomically, and unfinished memory deliveries leave rows pending.
External effects still require provider idempotency.

A payload contract reaches as far as the app that declared it. An event written
for a tenant some other app holds is checked against that other app's
declaration, which in a process that never composed it is no declaration at all:
nothing here invents a contract for an app that is not running, and such a write
is refused at the other end anyway — `Consume` will not run a handler for a
tenant its app does not hold, and the row belongs to that app's relay
([`catalog.go`](catalog.go)). Checking an event some app gave a payload type to
costs one read of `tenants` by primary key, on the publisher's own handle and
therefore inside its own transaction; an event no app typed costs the INSERT it
always cost.

Run `go test -race ./kit/events/transport ./kit/events/providers/...` for portable
envelope, memory delivery and local NATS TLS/credential checks. The SQL/broker
recovery tests remain in `kit/events`; run them with the development PostgreSQL
and NATS settings described in [Contributing](../../CONTRIBUTING.md#verify-at-the-relevant-boundary).
Reserve the broker for that suite: its stream-drift test changes and reconciles
shared stream settings. These checks do not qualify a deployed broker or a
downstream application's handler idempotency.

## Built on what came before

Decision 0022 asks a delivery to name what it composed rather than what it
rebuilt. **Reused:** the outbox's own `INSERT` in `write`, which is the one door
every event in the program already passes and is where the schema check lives;
`Schema.Validate`, `Schema.JSONValue` and the golden-file guard this repository
already had, each cure being a clause inside them rather than a new mechanism
beside them; `platformkit_handled`'s claims, which are what make the
`DeliverAll` re-delivery a log line instead of an incident; `dbtest.Schema` in the
new cases; and `module.KernelEvents`, adopted rather
than replaced when the merged `security.denied` had to be typed, with
`module.KernelName` added as the half that list was missing.
**Added:** `schema.go`'s projection of a Go type into JSON Schema, because no
existing unit could carry it — `kit/httpx/schemas.go` registers resource schemas
by hand and holds no `reflect` at all, so there was nothing there to extend; the
CloudEvents envelope on the wire, `transport.Event` having been a private struct
of tags; `events.Replay`, the outbox having had no operator's verb; and the two
nullable trace columns the envelope needs before the relay runs.
**Made reusable:** `transport.Filters`, so a provider that must read a rolling
window reads a list rather than re-deriving one; `events.Declare[T]`, which turns
a payload type into a compile-time dependency of the manifest that names it;
`Declared.Schema()` and `app.CoveredEvents`, so the coverage ratio is a number a
test reads rather than one a release note claims; `UPDATE_GOLDEN=1` as the only
writer of a checked-in contract; and `KernelName` with the `KernelEvents`
exemption in `Validate`, which is the shape the next kernel-emitted event arrives
in.
