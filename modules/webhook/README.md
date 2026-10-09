# webhook — signed outbound events from the outbox

A tenant registers an HTTPS address and the event types it wants. Every event of
those types is POSTed to that address, signed the way Standard Webhooks specifies,
retried for three days, and written to a log the tenant's administrator can read.

```
outbox ──▶ Fanout (kit/events handler, one subscription per emitted event)
              └─ one queued row per matching endpoint of this tenant
        ──▶ Deliver (kit/jobs, per tenant, every 5 s)
              claim ─ short tenant tx, FOR UPDATE SKIP LOCKED, commits
              send  ─ no transaction: the socket opens with no lock held
              outcome ─ new tenant tx: state, retry row, fault clock,
                        disablement + its event + its notice, one commit
```

## The receiver's check, in Go

The secret is `whsec_` + base64 of 32 random bytes, answered once by the create
and once by each rotation — Standard Webhooks' own spelling, which is what their
library expects as a key.

`webhook-signature` is a **space-delimited list of `<version>,<base64>` entries**,
and the comma is not decoration: the reference library splits the header on spaces
and then requires a comma in each part, so a header written `v1 <base64>` is one no
standard receiver accepts however correct its HMAC. This module answers one entry,
`v1,<base64>`; the list form is what leaves room for a second — a rotated-out key,
a v2 — without renaming the header.

```go
func verify(secret string, id, timestamp, signature string, body []byte) error {
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	if err != nil {
		return err
	}
	want := hmac.New(sha256.New, key) // HMAC-SHA256
	_, _ = want.Write([]byte(id + "." + timestamp + "."))
	_, _ = want.Write(body)
	at, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || math.Abs(float64(time.Now().Unix()-at)) > 300 {
		return errors.New("stale timestamp")
	}
	for _, entry := range strings.Split(signature, " ") {
		parts := strings.SplitN(entry, ",", 2)
		if len(parts) == 2 && parts[0] == "v1" && hmac.Equal(mustBase64(parts[1]), want.Sum(nil)) {
			return nil // and: deduplicate on webhook-id — delivery is at-least-once
		}
	}
	return errors.New("bad signature")
}
```

`modules/webhook/internal/signature_header_test.go` runs that algorithm against a
delivery this module actually sent, rather than against this module's own header
builder: a test that verified a signature with the function that signed it would
prove nothing.

Deduplicate on `webhook-id`. The module is at-least-once on the wire by
construction: a crash between the send and the transaction that records its
outcome loses the record of an attempt that physically happened, the lease
expires, and the row goes out again. `attempt` and `seq` make the duplicate
visible in the log.

## What is where

| | |
|---|---|
| `contracts/` | `Endpoint`, `Delivery`, the five events, the two permissions, `Service`, and the two ports: `Catalogue` and `Alerts` |
| `internal/service.go` | create, target, rotate, enable, disable, replay; the secret wrap |
| `internal/fanout.go` | the outbox handler: one event in, one queued row per matching endpoint |
| `internal/deliver.go` | the job: claim, send, sign, record, and the three-day fault rule |
| `internal/retry.go` | the ladder as a table: 1 m, 5 m, 30 m, 2 h, 6 h, 12 h, then a day |
| `internal/address.go` | the SSRF gate, one value read by the write and by the dial |
| `migrations/` | 000044 endpoints + secrets, 000045 the delivery log |

## Two resources, and what a caller may change

`GET/PATCH/DELETE /endpoints/{id}`, `GET /endpoints` and `GET /deliveries{,/{id}}`
come from `rest.Spec`; the delivery log offers **no CRUD write at all**, because a
delivery is a fact. Four routes answer with something other than the entity:

| Route | Why it is not a PATCH field |
|---|---|
| `POST /endpoints` | the answer is the signing secret, and a Spec answers with the row |
| `POST /endpoints/{id}/rotate` | same, and the entity must never hold the secret |
| `POST /endpoints/{id}/target` | both fields re-check the address policy and the catalogue |
| `POST /endpoints/{id}/enable`, `/disable` | the lifecycle: a status a PATCH could set is a flow of events nobody chose to stop |
| `POST /deliveries/{id}/replay` | queues a *new* row, `cause='replay'`, and answers with it |

The last three are `rest.Command`, which mounts the route and records the door on
the resource in one declaration. The first two are mounted by hand, because the
answer is a value the entity must never hold, and they record their own door
afterwards — `httpx.Surfaces.AddCommand`, the seam `rest.Command` itself calls.
A route that is in the API and not in `Resources()` is a door no generated screen
can draw and no native shell can find, which is how registration and rotation read
until the second review. Neither carries a `Run`: a derived screen performs a
write and redirects, and it has nowhere to show a value that exists in one
response and in no read, so ui/screens draws no button behind them and the two
doors stay JSON doors. See [Interface](#interface).

The secret is not a field of `Endpoint`. It lives in `webhook_endpoint_secrets`,
encrypted with a deployment key a tenant can neither read nor set, and the
plaintext exists only inside the process that signs. A `whsec_` value on the
entity would be a `whsec_` value on the generated form and in
`webhook.endpoint.created`, which the audit trail keeps for a year.

## Refusals worth naming

* An address that is not https, or carries a user name, a query or a fragment.
* A private or loopback address, unless `AllowLocalEndpoints` — a deployment
  flag with no column, field or route that can carry it. It never reaches the
  cloud metadata address, and the gate reads the **resolved** address, so a name
  that resolves to 127.0.0.1 is refused too. Such an attempt is `dropped`, not
  retried: retrying an address this deployment will never dial spends three days
  of somebody's queue to learn nothing.
* An event type this composition does not emit, named in the refusal. The list
  comes from the `Catalogue` port the composition writes.
* A row in another tenant: row level security answers 404, which says nothing
  about whether the row exists.
* A replay against a disabled endpoint: 409, and nothing is written.
* An endpoint that has failed for three days: `disabled`, its `queued` rows
  dropped with a reason, its history kept, its administrator told through the
  `Alerts` port — which a deployment may compose as nothing. A notice that *fails*
  is the same shape as a notice that is absent: it runs behind its own savepoint
  inside the outcome transaction, so the notifier's own half is undone and the
  endpoint, its outbox event and the log still commit. An error from that port
  would otherwise roll the outcome back, leave the attempt `in_flight`, and have the
  lease re-send it about every minute for as long as the notifier stayed down.
* A replay whose pair has no attempt waiting writes a new row; a pair with one
  already `queued` or `in_flight` is that command having been performed, and answers
  with that row. A replay that has been answered is a finished attempt, and asking
  again queues another: the partial unique index covers the two waiting states and
  no others.

## Authorization

### Permissions

The manifest in `modules/webhook/module.go` (`permissions`) declares two keys, whose constants are in `modules/webhook/contracts/permissions.go`.

- `webhook:read` guards the four generated read routes of the two Specs (`spec.Read`), the two generated screens `webhook/endpoints` and `webhook/deliveries`, and both nav entries (`Nav` in `Module`). It never reaches a secret: no readable field carries one.
- `webhook:update` guards every write. `endpointSpec.Write` covers the generated `PATCH` and `DELETE`; `httpx.Register(s.App, …, write, …)` guards the create and the rotate; and `rest.Command(…, rest.CommandOptions{Auth: write})` guards `target`, `enable`, `disable` and `replay`. A replay *sends*, so it is a write and never a read. The same `Auth` is what the two recorded doors are filtered by in the catalogue, so a caller who cannot call a route is not told the door exists.

The module declares no export. Its one subscription (`SubscribeAll: true`, `svc.Fanout`) and its two jobs (`webhook-deliver`, the retention sweep) are not requests and are guarded by nothing but the tenant transaction they open.

### Object scope

None. No command asks `tenancy.Policy` which row the caller may see: every read and write is inside the caller's own tenant transaction, and row level security on all three tables — `ENABLE` and `FORCE`, with the `platformkit_tenant_match` policy — is what answers "another tenant's row" with `ErrNotFound`. A row this tenant cannot see is a 404 on every surface, including the one whose body would have said whether it exists.

### Duties the module enforces itself

Rule 9, in every command, inside the transaction that writes:

- `Create`, `Target` re-check the address against `AddressPolicy` (with the deployment's flag) and every event name against the `Catalogue` port; both refuse with `crud.ErrInvalid` naming the value, and a `Create` with no actor is refused, because a disablement notice has to have somebody to address.
- `Rotate`, `Enable`, `Disable`, `Replay` take `crud.GetForUpdate` on the endpoint — the aggregate root — before reading any state they decide on, so two commands on one row settle in lock order and a stale read decides nothing.
- `Disable` and `Enable` are no-ops on a row already in that state: they write nothing and publish nothing, which is what makes two concurrent disablements publish exactly one event.
- `Replay` refuses a disabled endpoint with `crud.ErrConflict` (409) and queues nothing; a pair with a replay already `queued` or `in_flight` answers with that row and writes nothing; the partial unique index is the backstop for a caller that took no lock.
- `endpointSpec.Immutable` refuses `url`, `events`, `status`, `disabledAt`, `disabledReason`, `lastSuccessAt` and `createdBy` on the generic PATCH: the two fields that decide where events go are the `target` command's, and the rest belong to the lifecycle.

### Public faces

None. No route in this module is `httpx.Public()`, no route is on `s.Ops`, and no path literal names a surface: every route is on `s.App`, the tenant surface, and a request without a session is refused by the surface before this module's handler is reached. The module uses no `kit/limit`.

### The operator boundary

None. No entry in `permissions` sets `Operator: true`, and neither Spec sets `OperatorRead` or `OperatorWrite`: an installation's operators do not read a customer's endpoint list, and the delivery log holds that customer's own event bodies. `AllowLocalEndpoints` is deployment configuration rather than a permission — no column, field or route carries it — and the instance-metadata address stays refused whatever it says.

### Provisioning

The module declares no role. A composition grants the two keys through the tenant's roles (see `modules/auth/README.md`); in the reference application the seeded administrator role satisfies both, and neither of `apps/platformkit/roles.go`'s two task personas holds either — a module's doors are not a reason to widen somebody else's persona. The signing key and the two other deployment keys are read by `webhook.Module`'s provider out of `config.Webhook`.

## Composition

It is a kernel module: `apps/platformkit/app.go` names `webhook.Module` in `Use`,
beside `audit` and `task`, so its two screens, its routes, its subscription and its
two jobs are in the reference application. The provider reads three deployment keys
(`webhook.secret_key`, overridden by `PLATFORMKIT_WEBHOOK_SECRET_KEY`,
`webhook.allow_local_endpoints`, `webhook.retention_days`) and needs three answers:

```go
webhook.New(webhook.Deps{
	SecretKey: key,            // 32 bytes, read once from deployment config
	Catalogue: eventTypes,     // the names this composition emits
	Alerts:    notices,        // or nil: see the refusal above
	Tenants:   tenants,        // modules/tenant
})
```

`Catalogue` is answered by the composition as a literal — in the reference
application, `apps/platformkit/webhook_catalogue.go`, assembled from the emitting
modules' own constants, with
`TestTheWebhookCatalogueNamesWhatTheCompositionEmits` asking the resolved
composition whether the list still matches what it emits. `Alerts` is `Optional` and
the reference application composes nothing: it has no notice template for a machine
fault, and a deployment that needs the notice durable subscribes to
`webhook.endpoint.disabled`, which commits with the disablement.

Neither of `apps/platformkit/roles.go`'s two personas is granted either door:
they are the task module's, and `roles.go` says so — a module's doors are not a
reason to widen somebody else's persona. The tenant's `admin` role holds every
tenant-scoped permission, which is what lets an administrator register an
endpoint on day one; `apps/platformkit/persona_test.go` is the table of what each
persona may do and is refused. The signing key and the two other deployment keys
are read by `webhook.Module`'s provider out of `config.Webhook`.

## Reused

`kit/events`' `Subscription`, `Consume` and its claim row, mounted the way
`modules/audit` mounts its `SubscribeAll` handler; `kit/jobs.Job` with
`jobs.PerTenant` and `jobs.TenantLister`, the way `modules/task` takes its sweep;
`rest.Spec` for both resources' screens, routes, permissions and schema;
`kit/crud`'s `GetForUpdate`, `Create`, `Update` and `Classify` for every write;
`kit/httpx.Register` and `rest.Command` for the routes a Spec cannot answer with;
the ledger shape of `modules/notification`'s 000027 — a `bigint` identity for the
log's order, `clock_timestamp()`, RLS enabled and FORCEd; `entity.Base`,
`db.MigrationSource`, and `crypto/aes`, `crypto/hmac`, `crypto/sha256`,
`net/netip` and `net/http` from the standard library.

## Added

A subscriber registry, because nothing in the tree let a tenant name an address
outside the platform. HMAC signing of an outbound body, because the only `hmac`
use in the tree is `modules/auth`'s TOTP, which hashes rather than signs. An
address policy and a dial gate, because the inventory found no outbound HTTP
client outside `kit/flags` and no address rule anywhere. A three-day ladder,
because `kit/events`' own is five rungs and thirty seconds, terminal and internal
on purpose, and a customer's web server may be down for a weekend. The disable-on-a-fault-clock
rule, because nothing here disabled a tenant resource on a clock. The `Catalogue`
port, because `pkit.Composition` is reserved for the one after-everything module
and `modules/admin` holds it. A shown-once secret in its own encrypted table,
because HMAC needs the plaintext back and `kit/secrets` does not exist yet.

## Made reusable

Three things. `internal.AddressPolicy` is a deployment-level SSRF gate that reads
the resolved address and is a single value shared by the write and the dial — the
shape any module that ever POSTs somewhere a tenant named would want, and today
only this one has, because `internal/` is this module's and a neighbour would
have to take it from `contracts/` first. The claim is the shape, not that the
type is importable: nothing outside `modules/webhook` can read it as it stands. The claim/send/
record split in `internal/deliver.go`, with the advisory lock named for the
cross-row critical section and the lease that doubts an `in_flight` row, is the
shape for any module that must talk to somebody else's server on this platform's
clock without holding a lock open. And the `Catalogue` port with a literal in the
composition plus one drift test is the answer for any module that needs to offer
"what this application can do" as a list of names, which nothing else in the tree
could express while `modules/admin` holds the after-everything declaration.

## Limits

Stated because each one is a promise someone could otherwise read into the module.

* **At-least-once, never exactly-once.** A crash between the send and the outcome
  transaction loses the record of an attempt that physically happened; the lease
  expires and the row goes out again. `webhook-id` is what a receiver deduplicates on.
* **"Disable stops delivery now" is a re-ask, not a socket pulled back.** A tick
  claims its batch in one short transaction and sends the rows after it commits, so
  the sender asks the endpoint's own row again before each `POST`: a disable that
  lands while the first attempt of a batch is on the wire ends every row of that
  batch that had not started, `dropped` with `the endpoint is disabled`, and the one
  already open is answered as it was going. A disable that commits a microsecond
  after the re-check is one request; there is no protocol that promises less.
* **One waiting replay per pair, and no more.** The command is idempotent while an
  attempt is `queued` or `in_flight`; a pair whose replay has been answered can be
  replayed again, and that second replay is a new row with the next attempt number.
* **The window is counted from the current claim, not the first attempt.** The
  ladder's 12 rungs span about 4 days 20 hours, so an endpoint that fails one event
  while succeeding others (which resets `failing_since`) can retry that event for
  roughly 4.9 days. Closing the gap means carrying `MIN(attempted_at)` for the pair.
* **A redirect is followed only when it can still carry the event, and never off
  TLS.** `AddressPolicy` reads every hop, and two refusals live in that one call.
  A hop that steps down to plain HTTP is refused before the socket opens. So is a
  hop answering `301`, `302` or `303` to a `POST`: those three tell a client to ask
  the new address with a `GET` and **no body**, so following them posts an empty
  request that carries no CloudEvent and no signature over one, and the
  destination's `204` would be logged as `sent` for an event that never arrived.
  A `307` or `308` — which keep the method and replay the signed bytes — is
  followed to any host the dial gate will open, up to three hops. Either refusal
  ends the attempt `dropped`, with the address in its reason, and the signed body
  goes nowhere. `AllowLocalEndpoints` opens neither: the flag says this deployment
  dials a local *destination*, not that it carries a signature in the clear.
* **A delete cannot undo a request that reached its receiver.** The delete ends
  every `queued` row and hides the endpoint, but an attempt already on the wire
  gets its outcome written from the socket's own answer — `sent` and the status
  the receiver gave — in the same transaction that would otherwise have updated
  the endpoint's fault clock. An endpoint row that has gone is no reason to call a
  completed exchange `dropped` with status 0; the log says what happened and, for
  a non-success, that the endpoint was deleted, which is why no retry follows.
* **The SSRF gate refuses what a laptop and a cloud metadata endpoint look like,
  not every reserved block.** Loopback, link-local, private, unique-local and
  `0.0.0.0` are refused, resolved rather than textual; the metadata addresses stay
  refused under `AllowLocalEndpoints`. Not yet refused: NAT64 and 6to4 forms of
  those same addresses, benchmark, broadcast and multicast ranges.
* **A rotated secret is kept for a day and never signs with.** `previous_*` is
  stored so a support question can be answered; the space-delimited signature list
  would let the module sign with both keys during the window, and it does not, so a
  receiver mid-rotation must pick up the new secret.
* **The stored refusal sentences are English.** `contracts.Reason*` are written
  into rows. The two permission labels have pt-PT words; a reason on a delivery row
  is the language it was written in. Reason *keys*, translated at render, is open
  work.
* **The `Catalogue` port has one implementation, in the reference application**, and
  no exported fake for a deployment writing its own.

## Interface

Every route is on the tenant surface (`s.App`); there is no public and no operator
route, and no surface or prefix literal in this module.

| Door | Route |
|---|---|
| `webhook:read` | `GET /endpoints`, `GET /endpoints/{id}`, `GET /deliveries`, `GET /deliveries/{id}` |
| `webhook:update` | `POST /endpoints`, `PATCH /endpoints/{id}`, `DELETE /endpoints/{id}`, `POST /endpoints/{id}/rotate`, `/target`, `/enable`, `/disable`, `POST /deliveries/{id}/replay` |

Both resources are `rest.Spec` resources, so `GET /api/v1/app/resources` lists
them, the generated screens are `webhook/endpoints` and `webhook/deliveries`, and
the nav entries are read-guarded. The catalogue names every write this module
has, not only the CRUD ones: `register` and `rotate` on the endpoint, `target`,
`enable` and `disable` beside them, and `replay` on the delivery log — the last
two of the first pair named without a screen button, because a screen cannot show
a secret shown once and an administrator who cannot see the key has an endpoint
they cannot wire. The delivery log's Spec offers no CRUD write, and its one verb
is a command on the log that answers with a *new* row. Nothing in this module
renders HTML: `internal/ui` does not exist, because no page here is this module's
own.

## Verification

```
go test ./modules/webhook/... -count=1                  # acceptance, isolation, ladder, fault rule
go test ./modules/webhook/internal/ -count=3 -race -run TestTwoDisablesOfOneEndpointPublishOnce
go test ./apps/platformkit/ -count=1 -run TestTheReferenceCompositionPlacesTheWebhookModule
go test ./apps/platformkit/ -count=1 -run TestTheWebhookCatalogueNamesWhatTheCompositionEmits
```

`TestASubscriberReceivesASignedEvent`, `TestARetryAfterAFiveHundredIsDelivered` and
`TestAnotherTenantsEndpointNeverHearsTheEvent` are the brief's three acceptance
cases. Not run: `make e2e` (no journey writes an endpoint), the mobile journey (the
two screens are generated, and no flow names them), and every browser check — no
page here is this module's own to open.
