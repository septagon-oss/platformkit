# 0019: The tenant is in the object name

Status: accepted, 2026-10-01. It is the storage half of [ADR 0003](0003-tenancy-by-postgres.md):
row-level security bounds the rows, and this record decides what the names outside them look like
under the shared-instance posture of decision 0028. It adds no mechanism and enforces nothing the
kernel's own tenancy does not.

## Problem

Uploaded bytes leave the database and land in a store this repository does not
own: a directory on the installation's disk, or one bucket shared by every tenant
of a shared-instance deployment. Row-level security cannot reach them. The
enforcement the kernel has — `platformkit_tenant_match`, `ENABLE` plus `FORCE`
on the tables that name a file — stops at the row.

The port used to take a bare key. Consequences, each one real:

- Containment became a habit an adapter was expected to keep. `Local` derived
  the whole path from the key alone, so the only thing between a caller and
  another tenant's bytes was a regular expression.
- In one shared bucket the key names nobody. An access log line, a lifecycle
  rule, a bucket policy, a `ListObjects` prefix, a restore of one customer out
  of ten: none of them can be written against a name that carries no tenant.
- An orphan — a blob with no row, which an upload that failed after its blob
  write leaves by design — is a blob whose tenant must be discoverable from the
  store, or the sweep that removes it cannot say whose it was and cannot be run
  at all.
- One object name is also one deletion unit. "This subject's files are gone" and
  "this tenant's bytes are here" are both questions about a set of names.

## Decision

The tenant is in the object's name, and it is in the **port's type** rather than
in a convention.

`contracts.Storage` takes a `contracts.Scope` before every `Key`. A `Scope` is
one unexported UUID and there are exactly two doors that mint one: `ScopeOf`,
which reads the tenant the HTTP layer resolved for this request, and
`ScopeOfTx`, whose argument is a `db.Tx[db.Tenant]` — so a caller holding a
transaction opened against the whole installation does not compile here. A zero
`Scope` carries `uuid.Nil` and every implementation refuses it. An adapter is
handed a `Scope` and no `*gorm.DB` at all: it cannot query the database to check
what a caller claimed, which is the one respect in which the type is stronger
than the context it came from.

`Scope.ObjectName` is the one place a prefix and a key are joined —
`<tenant uuid>/<key>` — so the composition an adapter is asked to honour is the
one a test can read. `Local` puts it at `<dir>/<tenant uuid>/<2 hex>/<key>`,
which makes a tenant's bytes a directory: copying one out, listing what one
holds and removing one are walks rather than a scan of a million files that name
nobody. `Key` is a minted lower-case UUID and nothing else (`ParseKey`), so
there is no caller-supplied component in a path to escape with; a defined string
type cannot refuse `Key("../../etc/passwd")` at compile time, so every
implementation re-checks the shape on the way in, and `filetest.RunStorage`
feeds the malformed literal to each of them on purpose.

Three things this explicitly is not.

**It is not the security boundary.** The boundary is row-level security, which
follows the tenant on the context; `contracts/scope.go` says so and names the
limit: a caller that could forge a context could already open every row of that
tenant, because `db.Run` reads the same context. The scope is exactly as strong
as the kernel's own tenancy, which is the database's. The prefix is what makes
the boundary *legible* outside the database — in a log line, a bucket listing, a
restore — not what enforces it.

**It is not one bucket per tenant.** Tenants share a bucket and have separate
prefixes. A bucket per tenant is a fleet of buckets, a policy applied a
thousand times, and an operator's outage; a prefix per tenant is the same
isolation for the operations that need it — `List(prefix)`, `Get`, a lifecycle
rule, a copy, a delete — at the cost of one string. Where a deployment really
needs a bucket of its own, it composes a second store and points a second
`Deps.Storage` at it; the port does not care which.

**It does not make a key secret.** `files.storage_key` is `json:"-"`: the key
never leaves in a body. Publicness is `files.visibility` and nothing else, and a
public file is served by the module through the public surface, not by an
inference from an unguessable name.

## The consequences the decision has to pay for

An installation whose bytes predate the scope has them at
`<dir>/<2 hex>/<key>`. Nothing rewrites them — a rename across a store is a
migration no transaction can roll back — so `Local.Get` still reads the flat
layout and `Local.Delete` removes both names. `Local.Prove`, which answers the
erasure certificate's question, counts **both**: a copy still lying under the old
name is a copy that is still here, and a certificate stamped over a delete that
missed it would be a record that lied. A `Stat` that is neither present nor
absent is an error, because "cannot tell" answered as "gone" is the same lie in
a different coat.

The certificate is `file_erasures`: one row per removal, unique on
`(tenant_id, file_id, cause)` so a redelivery writes one certificate and not
two. `verified_at` is the whole of its promise and it is NULL until the store
answers that it holds nothing at that name; `versions_seen` is what the listing
counted, which is how a bucket with versioning switched on fails in the open
rather than passing quietly. A store that cannot answer at all is not refused its
erasure — the row is written with the column NULL, which is the difference
between "we checked" and "we assume". `Local` can answer, so it does.

What no certificate in this design covers is a store this module does not speak
to: a read replica, a cross-region mirror, a CDN that cached the bytes, or an
object lifecycle that already copied them elsewhere. Those are bucket posture,
they belong to the deployment, and an erasure receipt that claimed otherwise
would be worth exactly what it is worth.

The name is also the retention unit. `files.retention` maps a retention class to
how long it lives, the sweep walks each tenant inside that tenant's own
transaction — `jobs.PerTenant`, so RLS bounds every delete it makes — and a class
the deployment never priced is never deleted, only logged. Bytes are removed by
the subscription over `file.deleted`, after the row's transaction committed: a
delete that could be rolled back after the bytes left would be a download that
fails forever.

## What proves it

| Claim | Case |
| --- | --- |
| A key that is not a minted UUID is refused by every adapter that runs the suite | `filetest.RunStorage`, which feeds `../../etc/passwd`, `..`, `a/b`, `63://e/x` and an upper-cased UUID to each of them; `TestLocalStorageConforms` runs it over `file.Local` |
| One tenant's key opens nothing of another's, and an unlisted tenant is not even seen by the sweep | `TestAKeyOneTenantWroteOpensNothingForAnother`, `TestTheRetentionSweepRemovesWhatItsPolicyCovers` |
| The joined name is the one the store writes | `TestLocalStorageProvesWhatIsAtOneName`, which sees both layouts |
| A removal is certified only when the store says nothing is left | the same case, plus the certificate read at the end of `TestTheRetentionSweepRemovesWhatItsPolicyCovers` |
| A redelivery writes one certificate and commits | `TestARedeliveryCertifiesOneRemovalOnce` |
| The sweep is bounded by RLS and not by a `WHERE` | `TestTheRetentionSweepRemovesWhatItsPolicyCovers`: the tenant it was not listed keeps an expired file |
| An object store is in this tree, and what it still does not prove | `TestTheS3AdapterKeepsTheStorageContract` runs the same `filetest.RunStorage` suite over `internal.S3` against a live store; the adapter implements `Storage` and `Signer` and not `Prover`, so an S3 erasure's `verified_at` stays NULL and the versions, delete markers and abandoned parts under a prefix are a bucket lifecycle rule's and a future `Prove`'s |
