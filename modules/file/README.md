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
case UUID and nothing else (`contracts.ParseKey`). `Local` keeps bytes at
`<dir>/<tenant uuid>/<2 hex>/<key>`; the store that speaks to an object service
is wired by the composition, because `Deps.Storage` is a dependency and not a
constant. A deployment with `Local` gets no signed URLs: `Grant` answers
`contracts.ErrNotSignable`, which the route renders as 501, and that is the
honest answer for a disk store rather than a bug. The same store does answer
`Prover`, though: a directory holds one file under one name or none, so an
installation on disk gets a `verified_at` stamp on its erasure certificates
rather than one left open forever — and it counts both names `Delete` writes to,
so bytes still lying in the pre-scope flat directory keep a copy from being
certified away.

Compose it with `file.Deps{Storage, MaxBytes, QuotaBytes, ReconcileEvery,
Retention, Tenants, RetainEvery}`; `config.example.yaml`'s `files` section
supplies the directory, the upload ceiling and the per-tenant quota, and
`Retention` is the composition's table of how long each `kind` lives — a kind
the table does not name is never deleted, only logged. `Tenants` is required
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

The one read that is not a tenant's own is the orphan sweep: a blob no row names cannot be found from the rows, so `internal/reconcile.go` lists the store under `db.Tx[System]` and asks which keys no tenant's rows claim. It is a job with the ops surface's system token (`sweep.Use(s.Ops.SystemToken())` in `module.go`), reached from no route, and its delete is the one place this module removes bytes by tenant id rather than by a `Scope` a request brought.

### Duties the module enforces itself

- `Service.Open` refuses an anonymous caller a file that is not public, and answers `crud.ErrNotFound` rather than 403.
- `Service.Upload` mints its `Scope` from the context (`contracts.ScopeOf`), so nothing is open while a body arrives, and charges the tenant's quota and size ceiling (`charge`, `MaxBytes`).
- `File.Validate` in `modules/file/contracts/file.go` stamps `UploaderID` from `tenancy.ActorFrom`, refuses a `visibility` that is neither `private` nor `public`, and refuses a `kind` that is not a token (`contracts.ValidKind`, the same shape `files_kind` CHECKs) — which is the difference between a 422 naming the class and a 500 from the database.
- `Service.Delete` removes the row in the caller's transaction and publishes `file.deleted`; the bytes go after the commit, in the subscription. It refuses with `contracts.ErrHeld` while a live hold is on the file.
- `Service.Retain` and `Service.Release` lock the file row with `crud.GetForUpdate` before they write the hold, which is what makes "no live hold" a fact and not a race against the sweep. Placing one is a replace, never a second row (`file_holds` is unique on `(tenant_id, file_id)`); releasing a file with no hold is success, not an error.
- `Service.EraseSubject` deletes exactly the files this subject uploaded inside this tenant's transaction, `FOR UPDATE` over the set it is about to remove. One held file refuses the whole erasure with `ErrHeld` and names it, so a refusal writes nothing; a subject with no files answers a receipt of zero and writes nothing either. `fault()` in `internal/handler.go` answers `ErrHeld` with 409 on both doors that refuse it — `rest.Fault` knows the kernel's sentinels and none of this module's, so without that arm the caller's own hold came back as a 500.
- `internal.Sweep` (the retention job) is `jobs.PerTenant`, so each delete runs inside that tenant's own transaction. It rechecks the hold before each delete, and a kind with no configured policy is kept and logged.
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

### Provisioning

The code names no role. A composition grants `file:read` to people who may browse, download and mint a grant, `file:manage` to people who may upload and delete, `file:retain` to whoever may keep a file past its class, and `file:erase` to whoever answers a data-protection request — the last two are the ones a broad support role should not hold by default.
Roles are granted by permission key through the roles API (`PUT /api/v1/auth/roles/{name}`, cited in `modules/user/contracts/administration.go`) or in the composition's own role definitions.
This repository does not show a client `client.yaml` for this module.
