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

## The presentation vocabulary

`Icons`, `Tones`, `Formats` and `Visibilities` are the four closed lists a
resource's reading is declared in, and `ValidIcon`, `ValidTone`, `ValidFormat` and
`ValidVisibility` are their checks — the same shape as `Widgets` above, for the
same reason: the layer that draws an icon or colours a tone is `ui/`, which a
kernel package below the presentation layer may not import, so the kernel names
the words and a test inside `ui/` binds each name to a drawing
(`ui/icon/kernel_binding_test.go`, `ui/components/kernel_tone_test.go`).

The three levels that use them are `EntryHints` (a resource), `FieldHints` (a
field, declared by `ui:` directives and the `enumLabels`/`enumTones` tags in
`derive`'s existing grammar) and `CommandHints` (a lifecycle route). All three are
optional and all three are refused whole: `kit/rest/hints.go` names what does not
exist and `Spec.Mount`, `Singleton.Mount` and `rest.Command` panic at boot, so a
hint nobody honours never reaches a document. An entity that declares nothing
serialises exactly as it did before these types existed — `Field.Presentation` is
`omitzero`, and `ui/screens` prints a `presentation` key only for a hint somebody
wrote.

Two declarations can name one column's place on a list: `ui:"hide:list"` and
`visibility`. `Field.OnList` is the one place they resolve, and it resolves them
by precedence rather than by refusal — an explicit `visibility` is the narrower
word about one field's reading, so it wins in both directions, `shown` reclaiming
a column the older tag took and `detail` giving up one it left. With nothing
said, `hide:list` stands on its own. A `reference` names its target as
`module/entity`, and `kit/rest` keys that boot check by the same spelling for a
field and for a command's argument alike.

**Reused** — `Widgets`/`Presentations` and their `Valid…` predicates, `derive`'s
tag loop, `kit/entity/display`'s `FieldLabel`/`FieldHelp`, `ui/icon`'s `aliases`
seam and `ui/components`' `clBadgeTone`. **Added** — the four vocabularies and the
three hint structs, because no kernel list existed for an icon or a tone and a
refusal has to be against one, and the entry- and command-level wire objects,
because `httpx.{Resource,Command}` carried nothing a hint could ride. **Made
reusable** — the mount gate (`kit/rest/hints.go`), the declared-or-absent
predicate (`ui/screens/hints.go`) and `CheckReferences`, which answers the
reference question at boot where the whole resource list exists.
