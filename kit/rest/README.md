# Entity routes

`kit/rest` is an entity's projection onto HTTP. A module declares one `Spec[T]`
per entity in its manifest — module, entity, path, permissions and hooks — and
`Spec.Mount` registers list, create, read, update and delete on the
`httpx.API`, each with its declaration, emitting the events `Spec.Events()`
names and registering the `httpx.Resource` the admin generates screens from.
`Singleton[T]` is the same for a tenant's one row: a read and a PUT, no list
and no id in the path. Read [rest.go](rest.go) first;
[modules/task](../../modules/task/README.md) is the reference Spec.

Beside the five routes, `Command` mounts a verb on a row or the collection
with its own body and authorization, and `Operation` is a typed projection
with a custom path sharing the transaction and error handling. Updates and
deletes lock the live row first — an update before merging, validating and
snapshotting, a delete before removing the row — and a body that names no
column stops at the lock, so it neither writes, validates nor publishes.
`Fault` maps kit/crud errors to problem documents; `FieldErrors`, `Values`,
`UpdateValues` and `Writable` type a submitted form by the schema; `Display`,
`Text`, `Humanize`, `FieldLabel` and `FieldHelp` delegate to
[kit/entity/display](../entity/display/display.go), the one way a value is
shown, which the generated screens read without linking this package.

`Immutable` names the fields a command owns. Every door that reads a body — the
JSON create and patch, the two a page calls beneath HTTP, and `Values` beneath a
create form — asks its keys the decoder's own question: one folding onto a
declared field, `author`, `Author`, `AUTHOR`, answers 422 naming the field as
declared, and none of that body is stored. `UpdateValues` drops them instead: an
edit form renders them read-only and a browser posts a read-only control back.
`Singleton` declares none to refuse. A write that named no column changed
nothing, so it says nothing: no `UPDATE`, no validation, no event, `updated_at`
where it stood.

Prerequisites: an entity embedding `crud.Base` with a `TableName`, its
migration, and the permissions the manifest declares. Tests need the
development database: `make up`, then `make test TEST_PACKAGES=./kit/rest`.
