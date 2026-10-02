# File module

`modules/file` is uploaded bytes and the rows that name them. There is no
`rest.Spec`, because a file arrives as a stream: the routes under
`/api/v1/file/files` and the public read at `/api/v1/public/file/files/{id}` are
written in [internal/handler.go](internal/handler.go), guarded by `file:read`,
`file:manage`, `file:retain` or `file:erase`, with the admin entry at
`/app/file/files`. Where a blob write sits relative to the commit is the one
interesting problem here; [module.go](module.go) explains both answers, a
subscription removes the blob after its row and writes the proof, a sweep
reconciles what the two disagree about, and a second sweep removes the rows
whose retention class ran out.

The tenant is in the port's type. `contracts.Scope` names a tenant and is
minted at two doors only — `ScopeOf(ctx)` for a request whose tenant was
resolved at the middleware, `ScopeOfTx(db.Tx[db.Tenant])` for a command that is
already in a transaction — and `Storage` takes a `Scope` before every `Key`, so
an adapter is never handed a key and left to ask whose it is. A key is a lower
case UUID and nothing else (`contracts.ParseKey`). What the prefix buys, what it
explicitly does not buy — it is not the boundary, not one bucket per tenant, not
a secret — is [ADR 0019](../../docs/adr/0019-the-tenant-is-in-the-object-name.md).
`Local` keeps bytes at
`<dir>/<tenant uuid>/<2 hex>/<key>`; the store that speaks to an object service
is `S3`, and `Deps.Storage` is the composition's choice between them. A deployment
with `Local` gets no signed URLs: `Grant` answers
`contracts.ErrNotSignable`, which the route renders as 501, and that is the
honest answer for a disk store rather than a bug. The same store does answer
`Prover`, though: a directory holds one file under one name or none, so an
installation on disk gets a `verified_at` stamp on its erasure certificates
rather than one left open forever — and it counts both names `Delete` writes to,
so bytes still lying in the pre-scope flat directory keep a copy from being
certified away.

`S3` is the same port on any store that speaks S3 — AWS S3, Garage, SeaweedFS,
Ceph RGW — through minio-go, and it is the answer for an installation whose bytes
cannot live on one volume. Its object names come from
`contracts.Scope.ObjectName` and nowhere else, so one bucket shared by the whole
installation (decision 0028) still holds `<tenant uuid>/<key>`, and a UUID copied
out of one tenant's row names nothing in another tenant's prefix. That refusal is
inherited rather than re-written: `filetest.RunStorage` asks those questions of
every implementation, and `modules/file/s3_test.go` runs that suite against a live
object store. It is also the store that answers `Signer`, so
`GET /files/{id}/grant` returns a presigned read whose Content-Type,
Cache-Control and Content-Disposition are the object's own, written at `Put` by
`contracts.MetaFor` — the byte is served by the store, with no request of this
process in the path. Two doors it does not answer, both declared on the type
rather than discovered: it implements no `Reconciler`, so an installation on it
gets no orphan sweep and leaves an upload's abandoned bytes to a bucket lifecycle
rule, and it implements no `Prover`, so an erasure certificate's `verified_at`
stays NULL. It takes the length a stream does not declare, because that is the
only answer its own upload route gives: a body that arrives without one is
written, and the no-clobber promise is kept by claiming the object's name with a
conditional write of nothing before filling it, which is what a store's own
conditional create header cannot do across a multipart upload. The test stack
starts SeaweedFS (`make up`, published on 8333) and the adapter's cases fail
rather than skip without it, as the NATS transport's do — so every job that runs
`make check` starts that store as a step of its own, which is what
`modules/file/store_gate_test.go` holds the three workflow files to. The reference
application still composes `Local`: `make run` and `make e2e` are meant to work on
a machine with no object store, and which store a deployment has is a line its
composition writes.

Compose it with `file.Deps{Storage, MaxBytes, QuotaBytes, ReconcileEvery,
Retention, Tenants, RetainEvery}`; `config.example.yaml`'s `files` section
supplies the directory, the upload ceiling, the per-tenant quota and
`files.retention` — the deployment's table of how long each `kind` lives, which
`kit/config` parses as Go durations and refuses when an entry is keyed on no
class or is not a positive duration. The reference application ships the table
empty, so an installation that priced no class runs no sweep at all. A kind the
table does not name is never deleted, only logged. `Tenants` is required
with a policy, so a sweep that could not walk the tenants fails at composition
instead of quietly never removing anything. Consumers import
[contracts/](contracts/) and its [fake](contracts/filetest/), never `internal/`.
`make test TEST_PACKAGES=./modules/file/...` needs the development database.

## Authorization

### Permissions

The manifest in `modules/file/module.go` (`permissions`) declares four keys, defined in `modules/file/contracts/permissions.go`.

- `file:read` guards list (`file-file-list`), read (`file-file-read`), the authenticated download `GET` and `HEAD` `/{id}/content`, and the time-limited URL `GET /{id}/grant` (`file-file-grant`). It is also the "Files" nav entry and screen `file/files`.
- `file:manage` guards upload (`file-file-upload`, multipart, with the `visibility` and `kind` query parameters) and delete (`file-file-delete`).
- `file:retain` guards `POST /{id}/hold` (`file-file-retain`) and `DELETE /{id}/hold` (`file-file-release`). Placing a hold stops the clock for everybody else's deletion, so it is not something a reader does by default.
- `file:erase` guards `POST /erase` (`file-file-erase`). Erasing is a stronger promise than deleting — "and nothing is left", with the proof row that says so — which is why it does not borrow `file:manage`.

All are `httpx.Permission(...)` declarations in `modules/file/internal/handler.go`, and `kit/app` refuses to boot a route that guards itself with a key the manifest does not declare.

### Object scope

None. A search of the module finds no call to `tenancy.Policy` and no `Resource.Kind`.
Scope is the tenant, through row-level security, and it is carried in the type before it is carried in the SQL: `contracts.Scope` names one tenant, `Scope.ObjectName` is the only place a prefix and a key are joined, and both new tables (`file_holds`, `file_erasures`) are `ENABLE`+`FORCE ROW LEVEL SECURITY` under `platformkit_tenant_match(tenant_id)` (migrations/000034). The per-object rule is visibility, described under Public faces below.

The one read that is not a tenant's own is the orphan sweep: a blob no row names cannot be found from the rows, so `internal/reconcile.go` lists the store under `db.Tx[System]` and asks which keys no tenant's rows claim. It is a job with the ops surface's system token (`sweep.Use(s.Ops.SystemToken())` in `module.go`), reached from no route, and its delete is the one place this module removes bytes by tenant id rather than by a `Scope` a request brought. What the listing may name is narrower than what the adapter may read, and it is answered twice: `Local.tenantOf` answers only for bytes under this app's own segment (and for the flat directory), and `internal.foreignTenants` then drops every listed blob whose tenant's row names a different app — the directory says which store listed a byte, only `tenants.app` says whose tenant it is. A tenant whose row is gone names no app and stays its own store's to clean, which is `kit/events`' `holdsTenant` and the relay's answer for the same case. So on a volume two apps share, one app's sweep never reaches the other's, and a named app's bytes that still sit at the un-prefixed position before its `mv` are left by the deployment that names no app: they cost disk until the move, which is the way to be wrong about a directory two apps share.

### Duties the module enforces itself

- `Service.Open` refuses an anonymous caller a file that is not public, and answers `crud.ErrNotFound` rather than 403.
- `Service.Upload` mints its `Scope` from the context (`contracts.ScopeOf`), so nothing is open while a body arrives, and charges the tenant's quota and size ceiling (`charge`, `MaxBytes`).
- `File.Validate` in `modules/file/contracts/file.go` stamps `UploaderID` from `tenancy.ActorFrom`, refuses a `visibility` that is neither `private` nor `public`, and refuses a `kind` that is not a token (`contracts.ValidKind`, the same shape `files_kind` CHECKs) — which is the difference between a 422 naming the class and a 500 from the database.
- `Service.Delete` removes the row in the caller's transaction and publishes `file.deleted`; the bytes go after the commit, in the subscription. It refuses with `contracts.ErrHeld` while a live hold is on the file.
- `Service.Retain` and `Service.Release` lock the file row with `crud.GetForUpdate` before they write the hold, which is what makes "no live hold" a fact and not a race against the sweep. Placing one is a replace, never a second row (`file_holds` is unique on `(tenant_id, file_id)`); releasing a file with no hold is success, not an error.
- `Service.EraseSubject` deletes exactly the files this subject uploaded inside this tenant's transaction, `FOR UPDATE` over the set it is about to remove. One held file refuses the whole erasure with `ErrHeld` and names it, so a refusal writes nothing; a subject with no files answers a receipt of zero and writes nothing either. `fault()` in `internal/handler.go` answers `ErrHeld` with 409 on both doors that refuse it — `rest.Fault` knows the kernel's sentinels and none of this module's, so without that arm the caller's own hold came back as a 500. The `reason` the route is given is filed, not accepted and dropped: it goes into each `file.deleted` work order and lands in the proof row beside the digest, so "why was this person's file removed" is a column read and not a memory — `contracts.MaxErasureReason` caps it and a longer one is refused before any row goes.
- `internal.Sweep` (the retention job) is `jobs.PerTenant`, so each delete runs inside that tenant's own transaction. Its batch is a batch of the rows its policy covers: the class cutoffs (one `(kind, created_at <= cutoff)` arm per priced class) and the live-hold check are in the query, and both are rechecked before each delete, so `sweepBatch` costs latency and not a file kept past its class. A kind with no configured policy is never read and never deleted — the sweep reports what it removed and sees only the classes it was given.
- `internal.EraseBlobs`, the subscription, asks the store `Prove` after removing the key and writes `file_erasures` with `verified_at` NULL when the store still reports copies (`versions_seen` above zero) rather than certifying an erasure it could not check. The proof row is written `ON CONFLICT (tenant_id, file_id, cause) DO NOTHING`: Postgres aborts a transaction at its first failing statement, so a redelivery that caught a duplicate-key error would be a transaction that can do nothing else. Zero rows is the answer "already certified", and it writes no second `file.erased`.
- `Service.Grant` refuses an expiry over `contracts.MaxGrantExpiry`, a public file (`ErrPublicFile`, it already has an open door) and a store that cannot sign (`ErrNotSignable`). It changes no state, so it logs with the trace context instead of writing an audit row of its own.

The code shows no rule tying a delete, a hold or an erasure to the uploader.

### Public faces

`GET` and `HEAD` `/files/{id}` on the public surface (`httpx.Public()`, operations `file-file-public*`) return the bytes of a file whose visibility is public.
Any other file, and a host with no tenant, is a 404.
A public file is refused a grant (`ErrPublicFile`), so no signed URL ever competes with the open door. Download headers come from `modules/file/contracts/response.go`, and `contracts.MetaFor` hands the same set to the store at `Put` so a bucket-served object carries the `docs/cache.md` policy without a handler here.
Both public routes are reads; the module has no public write, so `kit/limit` is not applied.

### The operator boundary

None. No key in `permissions` sets `Operator: true`, and there is no `OperatorRead` or `OperatorWrite` route — `file:erase` and `file:retain` are tenant permissions, granted per role like the other two. The module's only cross-tenant reach is its own orphan sweep, which is a job holding the ops surface's system token (`s.Ops.SystemToken()` in `module.go`) and not a route anybody can call.

What an operator does instead of a route is two steps beside `make rehearse`: `make backup` writes one dump of the database — with the grants on its tables, because a restore the application cannot open is not a restore of it — plus a copy of the on-disk byte store and a `manifest.sha256` of every artefact, and `make restore-drill` puts a backup back into a scratch database and compares four things. The objects the installation's own store holds against the objects the backup carries, in both directions, and the installation's current bytes against the digests the manifest recorded (`--files`, which `make` passes for `PLATFORMKIT_FILES_DIR` when the directory exists, and which an object-store deployment omits because it has no directory here to name; given no `--from`, the drill forwards that same directory to the backup it takes for itself, because a drill that drilled a backup of its own making which never held those objects would refuse every clean installation that has one). Every restored object against the digest taken before the backup. The set of tables, and every table's row set, in both databases — a table either side names that the other does not is a restore that failed, not a drill that could not run. And the set of tables the application's *own role* can read in the source against the set it can read in the restore (`--app-url`, default `PLATFORMKIT_TEST_DATABASE_URL`): the owner connection every other check runs on holds every table by construction, so it cannot see a restore that came back shut against the installation. The drill prints `restore_drill_pass_ratio=N/M` (`scripts/backup.sh`, `scripts/restore_drill.sh`). The rows and the bytes live in two places, so a promise that the installation can be put back is only answerable by restoring it and then reading what came back as the thing that will use it. One backup is one database, never one tenant, so `make backup TENANT=acme` is refused rather than silently dropped. With no `--files` the byte half can only ask whether the backup still reads like itself, and the closing line says that instead of claiming the installation was opened.

### Provisioning

The code names no role. A composition grants `file:read` to people who may browse, download and mint a grant, `file:manage` to people who may upload and delete, `file:retain` to whoever may keep a file past its class, and `file:erase` to whoever answers a data-protection request — the last two are the ones a broad support role should not hold by default.
Roles are granted by permission key through the roles API (`PUT /api/v1/auth/roles/{name}`, cited in `modules/user/contracts/administration.go`) or in the composition's own role definitions.
This repository does not show a client `client.yaml` for this module.

## Built on what came before

Decision 0022 asks a delivery to name what it composed rather than what it rebuilt.
**Reused:** `kit/jobs.PerTenant`, wired as `modules/auth/internal/sweep.go` and
`modules/billing/internal/renew.go` wire it and with the same
`modules/tenant` `Active` as the lister; `contracts/filetest.RunStorage`, whose
signature changed once so that every implementation that runs it inherited the new
case — including both directions of the declared-length collision; `crud.GetForUpdate`,
`platformkit_tenant_match` and the `ENABLE`+`FORCE` shape `000019_file.up.sql`
already kept; `modules/audit`'s `SubscribeAll`, which is why an erasure needed an
event and not a new sink; `contracts/response.go`'s header set and
`contracts.MetaFor`, so an object served from a bucket carries the `docs/cache.md`
policy with no handler here; and the custom-format `pg_dump -Fc` artifact
`scripts/rehearse_migrations.sh` already reads. **Added:** `contracts.Scope` and
`contracts.Key` — the only scope-carrying value in the kernel is `db.Tx[db.Tenant]`,
which pins a connection for as long as a body arrives, and `Upload` writes bytes
before it opens anything; `Signer` and the presigned grant, because read, write and
delete was all the port had; `files.kind`, `file_holds`, `file_erasures`, the sweep
that reads them and the `files.retention` table that prices them; `internal.S3`; and
`make backup` with `make restore-drill`. **Made reusable:** `Scope.ObjectName` as the
one place a prefix and a key are joined, so no adapter is ever handed a bare key and
left to ask whose it is; `filetest.RunStorage` itself, which is the door any later
store walks through and where a two-implementation disagreement stops being an
argument; `Reconciler` and `Prover` as ports an implementation declines by name
rather than by silence; and `Deps.Storage`, which is still the composition's choice —
`file.Local(dir)` and `file.S3(cfg)` are the same call written in the same place.
