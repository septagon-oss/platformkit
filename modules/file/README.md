# File module

`modules/file` is uploaded bytes and the rows that name them. There is no
`rest.Spec`, because a file arrives as a stream: the routes under
`/api/v1/file/files` and the public read at `/api/v1/file/public/{id}` are
written in [internal/handler.go](internal/handler.go), guarded by `file:read`
and `file:manage`, with the admin entry at `/admin/file/files`. Where a blob
write sits relative to the commit is the one interesting problem here;
[module.go](module.go) explains both answers, a subscription removes the blob
after its row, and a sweep reconciles what the two disagree about.

Compose it with `file.Deps{Storage, MaxBytes, QuotaBytes, ReconcileEvery}`;
`config.example.yaml`'s `files` section supplies the directory, the upload
ceiling and the per-tenant quota. Consumers import [contracts/](contracts/) and
its [fake](contracts/filetest/), never `internal/`.
`make test TEST_PACKAGES=./modules/file/...` needs the development database.
