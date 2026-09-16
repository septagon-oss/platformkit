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
with a custom path that shares transaction and error handling. Updates and
deletes lock the live row before merging, validating and snapshotting. `Fault`
maps kit/crud errors to problem documents; `FieldErrors`, `Values`,
`UpdateValues` and `Writable` type a submitted form by the schema; `Display`,
`Text`, `Humanize`, `FieldLabel` and `FieldHelp` delegate to
[kit/entity/display](../entity/display/display.go), the one way a value is
shown, which the generated screens read without linking this package.

Prerequisites: an entity embedding `crud.Base` with a `TableName`, its
migration, and the permissions the manifest declares. Tests need the
development database: `make up`, then `make test TEST_PACKAGES=./kit/rest`.
