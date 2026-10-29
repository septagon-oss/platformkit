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

## Which catalogue measures a publish

A publication is checked before the INSERT against a manifest: the name has to be one
somebody declared, and the payload has to be the shape declared for it. Whose manifest it
is has two answers. The process's is what an install left standing under an app slug, and
`Publish` reaches it by reading `tenants.app` for the row's tenant — the placement the
relay and a delivery read too. The composition's is a value:
`events.NewCatalog(appname.Name, decls)` builds the app's own `Catalog`, and that value
publishes (`Catalog.Publish`, `Catalog.PublishFor`) and disposes (`Catalog.Close`).

`kit/app` builds one per composition and carries it — the way it carries the composition's
connection — on the context of every served request and of every job, subscription
delivery and relay tick the worker half runs, so a module's `events.Publish` is measured
against its own composition without knowing that anything was carried. `App.Publisher`
and `Runtime.Publisher` hand the same value to a composition that wants it in a module's
own `Deps`. With nothing carried the write answers from the installed catalogue exactly as
it always did, the `tenants.app` read included. Carrying chooses *which* contract answers —
a composition is held to the manifest it carries rather than to its own — and only
composition code carries one, so no actor, role or tenant reaches that choice. Delivery is a
different question and keeps its own answer: an event still cannot be handled by an app whose
tenant row the table does not show, and the catalogue is a contract, not a capability.

**Reused** — `ClaimApp`'s claim (now `DeclareFor`) with its grip counts, `standing`,
`retype`, `clashes`, `declaredSchemas` and `OneShapePerName`, so the constructor adds no
second lifetime ledger and no new refusal sentence; `httpx.WithConn`'s carrier shape; and
`dbtest.RunInOwnDatabase` beside `providers/memory` for this package's harness, with
`kit/app`'s own `compose(t)` configuration for its boot cases. **Added** — `Catalog`,
`Publisher`, `NewCatalog` and the context carrier,
because nothing on the shelf or in the kernel was both built by a composition and
consultable by the one write every event passes through. **Made reusable** — the carrier
and the `X` → `XFor` + `Deprecated:` shape for `RelayFor`, `SubjectFor`, `FilterFor`,
`FiltersFor` and `DeclareFor`, plus `KernelEventNames`, `IconNames`, `ToneNames`,
`FormatNames` and `VisibilityNames`: a composition, a module or a tool that needed the
process's answer now has one door to ask.

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
itself (migrations/000058), and the move asks for the exclusive one and refuses rather
than waiting — and a lock over every tenant that could still be the app's by
the end of the move — its own, and the ones no app is placed under, because a
placement is what turns an empty app into a slug and a slug never moves again —
taken before a row is read. The set that can join the app, and not the set that
already did, is the one a claim can be open in and still land inside it. A lock set over
a set others can join is only half of it: the walk that joins them declares the same
tenant key before it writes the row (migrations/000061), so a placement is either refused
over by the move or serialized behind it, and no tenant arrives inside an app whose move
is past its final read.
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
the door the locks were cut to close. Waiting for the trigger to say the tenant's piece is
the same hole one statement wider: an `INSERT` can wait for a relation lock, as `CREATE
INDEX`'s `SHARE` takes one, and the transaction declares nothing while it waits. So the
unscoped writer asks its tenant's key itself, in `declareTenant`, above the first read it
makes about the delivery rather than beside the row it may never reach — a lock taken after
a decision records it, and taken before, it makes the decision one the move has to answer.
Two shared locks per unscoped claim, and a moved deployment pays neither. A table lock is what this replaced, and it was wrong: two apps share
these tables, so one app's ordinary traffic refused the other app's move on every boot
and every job tick, and the window the move exists to close stayed open while both
apps' consumers ran. The move then takes the claims of the tenants this app holds — a
claim belongs to the tenant it was made in and a tenant belongs to one app
(`tenants.app`) — so a copy of each unscoped ledger onto the app's prefix carries
`handled_at`, `name`, `error` and `failed_at` verbatim, the purge ages on them, and the
delete that follows removes exactly the rows that copy read, by the keys that
copy read rather than by a predicate asked a second time (the tenant half of that
predicate answers about `tenants`, and the two askings sit either side of a write the move
does not own); the move then asks whether anything it had to rename is still unscoped
and refuses if it is — a question its own entry lock makes honest, because the naming
that could change its answer holds the key the refusal waits for; one `platformkit.ledger_moved`
record per tenant whose claims moved goes into the same commit. The claims of a tenant
whose `tenants.app` is empty belong to whichever deployment runs app-less beside this
one and are left where its consumer looks for them. A delivery mid-claim therefore
holds the durable and the move refuses, naming itself, having written and emitted
nothing — and what can now stand in front of it is app-less traffic in a tenant nobody
has placed under, which is the traffic a rollout that names its app stops, and the boot
gate waits it out on the retry it already had; and a delivery that names no app declares its tenant and then re-reads
`tenants.app` inside the transaction that writes its claim, because a tenant that took an
app while the delivery was being read is the one fact about it the placement changes
underneath it, and a second nonlocking read answers that just as fast as the first. Such a delivery writes
no claim and refuses its copy as the check before the transaction does: a mark under the
unscoped durable would be a mark in the one ledger that tenant's own app will never look
in, which is the window the move exists to close, marked instead of closed. A terminal
failure has no refusal available: its message is exhausted, and refusing the record would
leave that delivery pending for a redelivery refused again, with the failure kept nowhere,
so the dead letter moves rather than stopping — `deadLetter` declares its tenant, reads
`tenants.app`, and records the outcome under the durable that tenant's own app subscribes
and replays under, which is the transition above performs for rows that got there first.
A tenant nobody placed keeps the unscoped name, where the app-less consumer looks. `kit/app` runs
the step at boot and opens no consumer until it answers, so a refusal waits — the
worker that consumed over an unscoped ledger would replay events whose claims are
committed under the old name, which is the harm the move exists to close — and the
`ledger-move` job runs the step again on the scheduler's lock. An app that names
nothing has no prefix to move onto, so for it the step answers with the zero report and
kit/app neither schedules the job nor declares the event — see [`ledger.go`](ledger.go)
for the reasoning and the cases beside it. `kit/db`'s `phase=data` window refuses the
same act against a table keyed by anything other than the tenant, which is why this is a
drain its owner owns in a job rather than a migration.

The count the move refuses over is a column, and a column can still be on its way in: the
runner applies nothing of an owner behind a `phase=data` file until that file's drain
finishes, so an installation upgrading through a release that ships a backfill and an
expansion together runs this release's code over the ledger as it was before the
expansion. This step is the one that meets that state, because kit/app holds every
subscription and every job — that drain tick among them — until the move returns. So the
ledger is read for the column before it is read for the count: absent, nothing has ever
raised a count, the answer is zero by the shape of the write and the rename goes ahead;
present, the refusal above stands.

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

A durable is a transport address, and one tenant can be served under two of them:
the name a subscription carried before the tenant took an app, and the name it
carries after. A delivery asks both ledgers under both (`claim`), because the write
that makes a tenant this app's — another replica's placement — is the same write that
makes the tenant's pending rows this app's relay's, and the rename that would move the
mark under the newer name runs when the replica holding the tenant gets to it, which
may be after the delivery. What that reading cannot see is a mark that has not
committed yet, and two spellings are two primary keys: an app-less delivery still open
when its tenant is placed is not serialized against this app's copy of the same event.
Closing that needs a lock the placement waits for at exclusive strength — a placement
queued behind every open delivery in every tenant it is naming, which is a boot that
does not start — so the window is left as the at-least-once overlap stated above. It
ends with the app-less deployment it is a property of.

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

## Operation causality

`Event.Operation` carries the origin independently of the current event actor.
The outbox, relay and CloudEvents codec preserve `operationid`, `operationref`,
`operationtenantid`, `operationpersonid`, `operationoutcome` and
`operationrevision`. Partial or invalid metadata is refused; a fully absent
snapshot is historical. Publishing rechecks immutable identity against the
receipt and reads its current outcome in the same transaction. An expired
terminal snapshot can still be copied without creating another receipt.

Consume restores causality and clears it for historical events. It restores no
session or actor impersonation. Explicit completion and owner-designated terminal
callbacks share the existing delivery transaction. Audit copies the six facts
and exposes an exact operation-reference filter. [The receipt contract](../idempotency/README.md)
is the canonical explanation of transitions, admission and retention.

## The ack rule: a delivery owes its effects

Decision 0022, for this delivery. **Reused:** the claim in `platformkit_handled`
(`claim`, `000003`), the delivery context `Consume` already builds, the system-token
precedent of `deadLetter`, and the transport ladder in
`internal/delivery/policy.go` — no provider line changed, because `Sink.Handle`
already acks on nil and nothing else. **Added:** `Defer`, the `effects_owed` count
(`000075`) and `ErrDeliveryOwesEffect`: a claim used to know only *claimed*, and a
redelivery that arrived while the first attempt was still handing an effect over had
nothing to read, so it acknowledged work nobody finished. The debt is counted rather
than flagged because a handler may defer more than one effect and `kit/db` runs them as
one queue registered by different work: the count reaches zero only when the last of
them comes back, so a delivery that owes two is not acknowledged by the one that
finished first. **Made reusable:** a delivery that can say what it still owes, read
under its row lock, and the release that makes a failed effect come back rather than
vanish.

Two endings read that count rather than ignore it. The transport's ladder running out
over an owed effect writes its dead letter — `deadLetter` records the exhaustion beside
a claim already standing when the count above zero is what stopped every attempt — and
writes none of the count away, because a terminal record says work an operator has to
look at and not that the work finished. It is what keeps the outbox row, and so the
payload, past the purge, which exempts a row a dead letter describes, and
`events.Replay` stays the verb that recovers it. And `MoveLedger` refuses a rename over
a claim that still owes one: the rename makes a new primary key, so an outstanding
effect would be left naming a row the move deleted, the scoped twin would read as owed
nothing, and the debt would have been paid by moving it. The run after the effect comes
back moves the row, and the refusal is `ErrLedgerMoveContended`, which is the sentence
for "ask again".

## Composition of delivery steps

**Reused** — `Consume`, `MoveLedger`, `relayBatch`, `Schema.check` and the existing
claim, transport and telemetry helpers retain delivery behavior. **Added** —
private subscription, claim/handler, ledger publication, relay-row and JSON-shape
steps expose the ordering previously nested in long functions. **Made reusable** —
named internal steps operating on the caller's existing transaction and context.

[Subscription delivery](consume.go) validates and subscribes in order, then checks
tenant placement before claiming and handling. [Ledger movement](ledger.go)
locks tenants and durables before renaming both ledgers and publishing records.
[Relay](relay.go) reads locked rows, settles lag, publishes envelopes and stamps
only after publication. [Schema validation](schema.go) preserves null handling
and open projections. These steps add no transaction or transport boundary.

## Replaying one event

`Replay` is the operator's verb, and the reason it needs an actor is the reason it
needs a capability of its own (ADR 0006). A terminal failure writes a claim and a
dead letter in one statement, and every later delivery finds the claim taken and
skips the handler — that is the guarantee that makes at-least-once delivery safe to
run at all. Removing a claim is therefore removing the record that a consequential
action already happened.

The actor is required of the request's context rather than read from it, and before
the transaction opens, so a call with nobody behind it writes nothing, emits nothing
and leaves every claim and dead letter where they were. A system token says this
process may cross tenants; only an actor says a person asked (`kit/tenancy` names it,
`kit/httpx` puts it there).

The requirement is unconditional. A dead letter is reviewed late, so the row a replay
relays is usually old, and age invites the thought that an old enough one needs no
name. Whether a person has to be named for deleting the record of a finished delivery
cannot turn on how many days ago it finished — that would waive the audit exactly
where it is worth most. Who asked, and why, are the only questions the verb asks.

Asked, not granted. Permission lives behind `kit/httpx`'s per-route authorizer and
this verb has no route, so what it can require is that a person be named, not that
they hold a grant; reaching for an authorizer through a package-level variable would
be discovering a dependency rather than naming one. The product mounts the surface,
declares the permission, and asks the question there.

## Why a delivery checks the address it arrived on

A delivery arrives with three copies of one fact: the address the broker routed by,
the `app` the publisher stamped into the envelope, and the slug of the process reading
it. Two were never enough — a message written at this app's scoped address by a
publisher that believed it was publishing for another app is a message this app would
otherwise run a handler over, because an address says only where a message was written
down. So the document's own app is checked rather than trusted, and it is checked
first, because an operator reading the log has to learn which boundary disagreed.

Why this is needed at all: a consumer's filter fixes the module and event halves of
the subject, because it spells them out and a NATS `*` matches exactly one token, but
its tenant half is a wildcard by design. The tenant is therefore the one fact about a
delivered message the routing does not pin, and the envelope's own check that
`subject` is `Subject` of `tenantid` and `type` proves only that the document agrees
with itself — which is exactly what a self-consistent forgery satisfies. Without the
comparison, a message stored on tenant A's address and stamped as tenant B's would
open its transaction inside B's rows because its body said so: a tenant boundary held
up by convention, with a broker credential as its key.

Which of `Filters`' addresses pass depends on whether the reading app names itself,
and the two cases are one rule read twice. An app that names itself passes only its
own scoped address; the older two name no app, and an address that names no app
cannot be shown to be this app's, because it is the address every other app's
previous build published at too. That is decision 0074 rule 7's "delivery's app check
applied to both": the filter stays wide for the length of a rollout precisely so that
the boundary, and not the filter, is what decides. An app that names nothing — the
deployment of one app, which is every composition that sets no slug — passes both
pre-decision-0074 addresses, as it always has, and refuses any address carrying an
app token, because it has no app to agree with and scoped traffic is somebody else's.

The wide filter costs a real deployment something, and the cost belongs to the
operator: a process must not start naming an app until no process still publishing at
the older addresses is alive, because from that moment its own older traffic is
unreadable to it. `kit/appname`'s Limits carry the same rule.
