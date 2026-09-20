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

## The JSON Schema projection

`JSONSchema(fields)` projects a field list onto JSON Schema 2020-12 — the shape
an OpenAPI document, an MCP tool definition or a form validator already reads,
none of which has to learn what a `FieldType` is. It returns an object schema
that admits no property nobody declared. `TypeString` and `TypeText` become a
`string`, `TypeInt` an `integer`, `TypeFloat` a `number`, `TypeBool` a
`boolean`, `TypeTime` a string of `format: date-time`, `TypeUUID` a string of
`format: uuid`, and `TypeList` an `array` of whatever `Elem` maps to. `Enum`
becomes `enum`, `Doc` a `description` and `ReadOnly` sets `readOnly`; the
required names are the ones a caller must send, so a field the server owns is
not among them. A `Default` arrives as the property's own JSON type — `"3"`
beside an `integer` is `3`, `"true"` beside a `boolean` is `true` — and a
default that will not parse as the type beside it is a returned error rather
than a string where a number belongs. A `Widget` travels as
`x-platformkit-widget`, and a `text` column with none declared gets `textarea`,
since a paragraph and a line are the only difference a control cares about.
`HideList` and `Present` are not in it: which screen shows a column, and how a
value reads, are not facts about the record a caller sends.

The function is pure, deterministic, and derives everything on the call —
`Field` stays authoritative and the document is stored nowhere, so there is
nothing to keep in step. It guarantees the shape and JSON type of a record the
API accepts and the names that cannot be omitted. It does not say who may send
the record, which transition a value permits, that an instant or identifier is
one because it matches its `format`, or what an amount is denominated in:
[ADR 0012](../../docs/adr/0012-independent-parts.md) keeps those with the
product. `testdata/task.schema.json` is the projection of the task entity's
fields, kept honest by `TestTaskSchemaIsTheProjectedShape` in `modules/task`.
`ui/screens` and `kit/app` carry the projection beside the fields each already
carried.

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
