# One door to a deployment's secrets and keys

Every module that needed a credential read its own environment variable for it, and
every cloud keeps those bytes somewhere else. This package is the port for both
questions a deployment is asked — *what are this name's bytes, and which version of
them*, and *wrap this plaintext under that key and give it back* — with three adapters
beside it, one conformance suite every adapter runs, and a name grammar that makes the
one refusal of path traversal possible in one place.

A read carries the tenant that may make it: an address is either the installation's
(`Shared`) or a customer's (`Of(tenant, …)`), no caller can build one without saying
which, and a read whose context resolved a different tenant is refused before anything
is looked up. What every read writes is the name, the adapter and a length — never a
byte — so a trace collector cannot become a secret store by being one. Sealing and
opening are auditable at the key for the same reason: a key's name and the number of
bytes it wrapped are facts; the plaintext is not.

**Reused**: `kit/cache`'s shape end to end — a typed address built only by `Of`/`Shared`
(`cache.Scope`), sentinels with their judgement written into the doc comment, a
provider package under `providers/`, one conformance suite every adapter runs
(`cachetest`'s `Store`/`Failing` closure form), and the valkey triple of
`compose.yaml` service + `Makefile` address + named-reason skip with a pin test that
refuses their disappearance. The envelope body is `modules/webhook`'s `box`
(`internal/sign.go`), whose own comment says a future `kit/secrets` has exactly this to
replace; the address as additional authenticated data is the one thing added, and it
closes the gap that comment records for itself. `kit/telemetry`'s `Tracer` and
`SpanAttrs` are the trail, and `kit/tenancy.FromContext` is the scope check.

**Added**: three things nothing on the shelf carried. A port with a *version* beside
the bytes (`modules/auth`'s `contracts.Secrets.Lookup` is one method, no version, no
error and no tenant, and it cannot carry a rotation signal it never had); a second half
that seals and opens, which no module here does except webhook, for webhook's own
signing keys and no anyone else's; and the conformance suite's `Exact` fixture — each
adapter declares the bytes its store can hold unchanged, so the difference between a
mounted file and a JSON field is a stated claim rather than a skipped case. `kit/porttest`
was not composed: its generated cases need `Mutates`/`Snapshot`/`Publishes`, and this
port writes no row and emits no event.

**Made reusable**: `secrets.Map` and `secrets.Broken`, so a module's own test can
resolve a secret and simulate an outage without importing an adapter — the notification
module's SMTP case already runs on them; `secrets.SealWith`/`OpenWith`, the local
envelope written once for any store that holds its own key material; `secretstest`'s
five entry points, which a fourth adapter (AWS, GCP, Azure) runs unchanged; and
`config.Secrets` plus the six `keys` rows, which is the one door an outside name comes
in through.

## Adapters

| `secrets.adapter` | a secret is | a version is | a tenant's secret | rotation |
|---|---|---|---|---|
| `env` (the default) | `PLATFORMKIT_MAIL_PASSWORD` for `Shared("mail.password")` | none: `""` | refused: `ErrNotSupported` | re-seal everything |
| `file` | `<dir>/<name>`, or `<dir>/<tenant>/<name>` | the file's mtime, UTC RFC3339Nano | one subdirectory per tenant | re-seal everything |
| `openbao` | KV v2's `value` field at `secret/data/<name>` | `v<N>`, the mount's own version | the tenant segment in the KV path, its UUID as the prefix of the key's name, and the transit derivation context | the key service's: old ciphertexts still open |

An unset `secrets.adapter` is `env`: the environment is the one store every deployment
has, it opens nothing, and a composition is never refused for a secret store it did not
name. `secrets.token` is the one credential that cannot arrive through the port — the
port needs it to read the port — which is why it is an environment override and not a
name in the store.

No new Go dependency: KV v2 and transit are three JSON calls, and the vendor SDKs
(`github.com/openbao/openbao/api/v2`, `github.com/hashicorp/vault/api`, both MPL-2.0)
arrive with some 244 packages for them. The `openbao/openbao` container is test-only,
pinned by digest in `compose.yaml`, named for its licence there as SeaweedFS is; nothing
this repository ships links or bundles it.

## Composition

```go
pkit.Provides[secrets.Secrets]()
pkit.Provides[secrets.Keys]()
secretsStore, err := secretStore(cfg) // env, file or openbao, by secrets.adapter
pkit.Put[secrets.Secrets](w, secretsStore)
pkit.Put[secrets.Keys](w, secretsStore)
```

`apps/platformkit`'s `secretStore` is the one place the reference application names a
store, and every adapter is imported there: a composition that could name two of three
would silently be unable to run the third. A module takes
`pkit.Needs[secrets.Secrets]()` and resolves a name where it needs the bytes —
`modules/notification`'s SMTP carrier at dial time, so a plan is never refused for a
secret the process has not needed yet and a rotation reaches the next send.

## Limits

- **`env` has no version and no tenants.** An environment variable has no token for
  which bytes it holds, and a flat process environment cannot answer one value per
  customer. Both are the deployment's facts, not the port's: mount `file` or set
  `secrets.adapter openbao`. The suite's version and tenant cases run on the adapters
  that answer them, and `env`'s own package asserts both refusals positively.
- **A KV v2 `value` is a JSON string.** Bytes that are not valid UTF-8 cannot live in
  OpenBao's KV at all, so the OpenBao run of the suite declares its own `Exact` set
  rather than skipping the byte-for-byte case. A raw key is not stored in a KV field;
  it is what a key service is for.
- **`file` follows symlinks, and refuses where they land.** A mounted Secret volume
  *is* symlinks — every data file points through `..data` — so refusing links would
  refuse the deployment this adapter exists for; what is refused is a resolved path
  outside the store, which is the property that mattered.
- **No event, no metric, no cache, no transaction.** A read has no domain state, so an
  event would be a claim about something that already happened, committed in a
  transaction of its own — the pair that can disagree. The audit trail is event-fed and
  commits its row beside the domain state the event describes; and this package imports
  no module — `modules/audit` is above it, not beside it — so it could not write that
  row itself even if the row made sense. The sealed event therefore belongs to the
  command that seals a value and stores the ciphertext, in its own transaction, beside
  the row that holds the bytes. A metric is none of the runtime's three numbers — slow,
  behind, refused — and the per-call trail already answers "how many" with the tenant
  and the request attached. A cached secret is a secret whose rotation nobody sees, so
  every read reaches the store and `Version` is the signal a caller with a real
  invalidation path may build a memo on. A lookup happens before there is a transaction
  to join, and a read that joined the caller's would hold a secret's lifetime open
  across a commit. The package's `doc.go` states the same four in the short form a
  reader at the source sees.
- **One name grammar, refused in one place.** `Of` and `Shared` refuse the name in this
  package rather than in an adapter, so no adapter can be wrong about it. The subset is
  the intersection of what a file name, a URL path segment and an environment variable
  can all spell, which is what lets `env`, `file` and OpenBao address one name; and it
  is the one place traversal is refused, so an adapter that joins a name into a path has
  nothing left to escape from.
- **One name is the port's: `mail.password`.** `auth.factor_key`,
  `webhook.secret_key`, `bootstrap.password`, `nats.password`, `cache.password` and a
  DKIM key still arrive through `kit/config`, and `modules/auth`'s
  `EnvironmentSecrets` is still its own one-method port. Moving a port and six secrets
  in one change is a change nobody can review; webhook's `box` and auth's port are the
  next two in line, in that order.
- **The OpenBao container runs dev mode**, unsealed and in-memory, and
  `deploy/openbao-dev.sh` mounts KV v2 at `secret` and transit at `transit` before the
  healthcheck is satisfied — dev mode mounts the first and not the second, and a stack
  that came up without `transit` would read as a broken adapter rather than as an
  unmounted engine. `PLATFORMKIT_TEST_OPENBAO_URL` is exported only when that port
  answers the seal-status question an OpenBao answers and nothing else, and the suite
  skips with that sentence when it is not — a probe of the port alone would point the
  suite at whatever else a stack happens to have published there.
- **A key's address in the transit mount is one path segment.** The engine refuses a
  nested key name — `transit/keys/<tenant>/<key>` answers 404 `unsupported path`,
  measured against the container — so a customer's key is `<tenant-uuid>-<name>`,
  prefixed rather than nested, and a policy reads `transit/encrypt/<tenant>-*`. KV v2
  keeps the nested `<tenant>/<name>`: its paths nest freely and per-customer policy is
  better served by a directory there.
- **The two key address spaces are two spellings, and that is the whole of the
  binding when a mount has none.** A name is legal bytes for a hyphen and a UUID, so
  `Shared("<tenant-uuid>-relay")` is a name somebody could write, and it is exactly
  how `Of(tenant, "relay")`'s key is spelled. `KeyAddress` therefore writes the
  installation's own keys under `shared-`, which no canonical UUID begins with, and so
  no tenant address can be.
- **Whether the derivation is real is the mount's, not the adapter's.** A key created
  `derived: true` refuses a call with no context, and a key created without that flag
  answers a call that carries one by ignoring it — both measured against the
  container. The tenant's own key *address* is therefore the binding that holds
  whatever the mount looks like; the derivation context is the second one, and a
  deployment that wants it mounts a customer's keys derived and the installation's own
  not. `secretstest.Keys` and `TenantKeys` run against the container on those terms.
