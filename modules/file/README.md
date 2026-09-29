# File module

`modules/file` is uploaded bytes and the rows that name them. There is no
`rest.Spec`, because a file arrives as a stream: the routes under
`/api/v1/file/files` and the public read at `/api/v1/public/file/files/{id}` are
written in [internal/handler.go](internal/handler.go), guarded by `file:read`
and `file:manage`, with the admin entry at `/app/file/files`. Where a blob
write sits relative to the commit is the one interesting problem here;
[module.go](module.go) explains both answers, a subscription removes the blob
after its row, and a sweep reconciles what the two disagree about.

Compose it with `file.Deps{Storage, MaxBytes, QuotaBytes, ReconcileEvery}`;
`config.example.yaml`'s `files` section supplies the directory, the upload
ceiling and the per-tenant quota. Consumers import [contracts/](contracts/) and
its [fake](contracts/filetest/), never `internal/`.
`make test TEST_PACKAGES=./modules/file/...` needs the development database.

## Authorization

### Permissions

The manifest in `modules/file/module.go` (`permissions`) declares two keys, defined in `modules/file/contracts/permissions.go`.
`file:read` guards list (`file-file-list`), read (`file-file-read`) and the authenticated download `GET` and `HEAD` `/{id}/content`, and the "Files" nav entry and screen `file/files`.
`file:manage` guards upload (`file-file-upload`, multipart) and delete (`file-file-delete`).
All are `httpx.Permission(...)` declarations in `modules/file/internal/handler.go`.

### Object scope

None. A search of the module finds no call to `tenancy.Policy` and no `Resource.Kind`.
Scope is the tenant, through row-level security. The one per-object rule is visibility, described below.

### Duties the module enforces itself

`Service.Open` in `modules/file/internal/service.go` refuses an anonymous caller a file that is not public, and answers `crud.ErrNotFound` rather than 403.
`File.Validate` in `modules/file/contracts/file.go` stamps `UploaderID` from `tenancy.ActorFrom`.
`Service.Upload` charges the tenant's quota and size ceiling (`charge`, `MaxBytes`), and `Service.Delete` removes the row in the caller's transaction and the bytes after commit (`RemoveBlob`).
The code shows no rule tying a delete to the uploader.

### Public faces

`GET` and `HEAD` `/files/{id}` on the public surface (`httpx.Public()`, operations `file-file-public*`) return the bytes of a file whose visibility is public.
Any other file, and a host with no tenant, is a 404.
Download headers are set in `modules/file/contracts/response.go`.
Both are reads; the module has no public write, so `kit/limit` is not applied.

### The operator boundary

None. Neither permission sets `Operator: true`, and there is no `OperatorRead` or `OperatorWrite` route.

### Provisioning

The code names no role. A composition grants `file:read` to people who may browse and download, and `file:manage` to people who may upload and delete.
Roles are granted by permission key through the roles API (`PUT /api/v1/auth/roles/{name}`, cited in `modules/user/contracts/administration.go`) or in the composition's own role definitions.
This repository does not show a client `client.yaml` for this module.
