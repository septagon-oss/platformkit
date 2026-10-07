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
and once by each rotation. Standard Webhooks' own spelling, so a customer's
existing receiver library verifies it unchanged.

```go
func verify(secret string, id, timestamp, signature string, body []byte) error {
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	if err != nil {
		return err
	}
	want := hmac.New(sha256.New, key) // HMAC-SHA256
	_, _ = want.Write([]byte(id + "." + timestamp + "." + string(body)))
	if !hmac.Equal(want.Sum(nil), mustBase64(strings.TrimPrefix(signature, "v1 "))) {
		return errors.New("bad signature")
	}
	at, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || math.Abs(float64(time.Now().Unix()-at)) > 300 {
		return errors.New("stale timestamp")
	}
	return nil // and: deduplicate on webhook-id — delivery is at-least-once
}
```

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
come from `rest.Spec`; the delivery log offers **no write at all**, because a
delivery is a fact. Five routes are this module's own:

| Route | Why it is not a PATCH field |
|---|---|
| `POST /endpoints` | the answer is the signing secret, and a Spec answers with the row |
| `POST /endpoints/{id}/rotate` | same, and the entity must never hold the secret |
| `POST /endpoints/{id}/target` | both fields re-check the address policy and the catalogue |
| `POST /endpoints/{id}/enable`, `/disable` | the lifecycle: a status a PATCH could set is a flow of events nobody chose to stop |
| `POST /deliveries/{id}/replay` | queues a *new* row, `cause='replay'`, and answers with it |

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
  `Alerts` port — which a deployment may compose as nothing.

## Composition

```go
webhook.New(webhook.Deps{
	SecretKey: key,            // 32 bytes, read once from deployment config
	Catalogue: eventTypes,     // the names this composition emits
	Alerts:    notices,        // or nil
	Tenants:   tenants,        // modules/tenant
})
```

Nothing outside this repository composes it yet, and that is deliberate: with one
client in this tree, wiring it into the reference application would make it a
module composed by exactly one client — a bespoke module. It lands uncomposed and
generic, with the two ports a product fills.

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
the resolved address and is a single value shared by the write and the dial — any
module that ever POSTs somewhere a tenant named can take it. The claim/send/
record split in `internal/deliver.go`, with the advisory lock named for the
cross-row critical section and the lease that doubts an `in_flight` row, is the
shape for any module that must talk to somebody else's server on this platform's
clock without holding a lock open. And the `Catalogue` port with a literal in the
composition plus one drift test is the answer for any module that needs to offer
"what this application can do" as a list of names, which nothing else in the tree
could express while `modules/admin` holds the after-everything declaration.
