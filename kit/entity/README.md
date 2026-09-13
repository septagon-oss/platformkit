# Entity definitions and fields

`kit/entity` owns the shared `Base`, the `Entity` constraint and the field
metadata derived from Go structs. It needs no application, database or web
server. Its only dependency outside the standard library is `google/uuid`.

For a plain form or command input, derive fields directly from its type:

```go
type Input struct {
	Title string `json:"title" validate:"required" doc:"A short title"`
	State string `json:"state" enum:"open,done" default:"open"`
}

fields := entity.FieldsOf(reflect.TypeFor[Input]())
```

Import `reflect` and `github.com/septagon-oss/platformkit/kit/entity` for this
example. Use `Fields[*YourEntity]()` for a struct that embeds `entity.Base` and
implements `TableName() string`. Each call returns its own fields, enum slices
and reflection index paths, so presentation changes cannot alter another
consumer's schema. `FieldNamed` looks up a field by its JSON name.

Existing `crud.Base`, `crud.Entity`, `crud.Validator`, `crud.Schema` and field
types are aliases. Existing CRUD metadata helpers forward here. Applications
can migrate those imports independently while keeping their storage calls.

`BaseOf` lets a storage adapter reach the embedded metadata; it does not grant
tenant access. CRUD retains tenant stamping, validation-error mapping and SQL
operations. Tags still describe storage columns and presentation hints, but
schema derivation neither writes a value nor executes those operations.
