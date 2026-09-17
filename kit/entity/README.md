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

[`display`](display/display.go) is the sibling that turns a field's value into
words: `Text` for a control, `Display` for a person, `Humanize` and `FieldLabel`
for a name. It depends on this package alone, so a renderer of schema values
needs neither CRUD nor REST.

`BaseOf` lets a storage adapter reach the embedded metadata; it does not grant
tenant access. CRUD retains tenant stamping, validation-error mapping and SQL
operations. Tags still describe storage columns and presentation hints, but
schema derivation neither writes a value nor executes those operations.

## The widget vocabulary

`ui:"widget:select"` names the control a screen draws. `Widgets` is every name it
may carry, and `ValidWidget` is the check; `ui/forms` renders one control per
name, and `widget_test.go` there refuses a name in this list with no drawn
control. What enforces the pair is `rest.Spec.Mount`, which panics at boot over a
field naming anything else.

Before that, an unknown name drew a plain text input and said nothing, so
`widget:file` was an upload control no schema could reach while the component
behind it worked when built by hand in Go. `datetime` and `checkbox` are names
the foundation's own entities, the catalog and a client already carry on fifteen
fields, where the Go type chose the control and the name was ignored; both now
mean the control, on a string holding an instant as much as on a `time.Time`. Run
`go test ./ui/forms ./kit/rest` to see the vocabulary, the drawing and the refusal
together. Neither proves a control is right for the field: `entity-picker` draws
the identifier text box and says there is no picker yet.
