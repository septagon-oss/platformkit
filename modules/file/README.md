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

## Where the bytes go

Two adapters implement `contracts.Storage`, and the choice is a wiring decision in `file.Deps{Storage}`:

- `file.Local(dir)` keeps the bytes on a local disk. One process, one directory; it also implements
  `contracts.Lister`, so the reconciliation sweep reclaims bytes a failed upload left behind.
- `file.S3(file.S3Config{...})` keeps them in any S3-compatible object store (AWS S3, SeaweedFS, Garage, Ceph
  RGW) through `minio-go`. Every object is `<tenant id>/<file id>`, the tenant read from the request's context,
  so a file id copied from one tenant's row names nothing in another's prefix, and a call that resolved no
  tenant is refused. It does not implement `contracts.Lister` — an installation-wide listing of a shared bucket
  is the reach it exists to refuse — so orphans are reclaimed by a bucket lifecycle rule instead: expire objects
  that no row references, or simply those older than the upload timeout under an `incomplete/` policy of the
  store's own.

`make up` starts SeaweedFS beside Postgres and NATS, and the S3 adapter's tests run against it
(`modules/file/s3_test.go`), failing rather than skipping when it is absent.
