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

A declared word is translated without being keyed twice. The copy key is
*derived* from the declaration's own address — `hints.<module>/<entity>.<aspect>`
(`hints.task/task.field.dueAt.label`, `hints.task/task.command.assign.label`) —
and the literal in the tag or the `Present:` struct is its own English fallback,
so no `en.json` exists anywhere and no author writes a key beside a sentence they
already wrote. The resolver is `ui/resource/hints.go`, shared by the catalogue
document and the generated screens; the vocabulary names an icon, a tone, a
visibility or a field name are never resolved, because they are drawn and not
read. Write a label only where it differs from `Humanize(name)`.

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

## Review decks

`EntryHints.Deck` declares a review reading for a list resource. `Queue` uses
existing repeated `filter=field:value` equality filters: the first colon splits
the field, other colons and commas are literal, different fields are ANDed, and
the last duplicate field wins. A nil queue reads all rows; a declared empty
queue is refused. `Order` is an existing sort value (`title` or `-title`), naming
a visible scalar field also listed in `Sortable`; the list retains its id tie
breaker. `DeckFault` and the list adapter share `Coerce` for scalar values.

`Directions` maps any of `right`, `left`, `up`, `down` to a command, label, tone
and icon from the existing vocabulary. The command must be a non-system row
command on that resource, with no inputs or explicit decodable defaults for
every input. Optional inputs without defaults do not qualify. The alternative
`down: {Skip: true}` has no other fields and sends no command: a client moves
the card to the back of its current queue without changing stored order.

`rest.Spec.Mount` checks the local declaration; `httpx.API.ValidateDeclarations`
checks command references after all registrations, before boot effects.
`rest.CheckRowHints` delegates to that same decision. Singleton, commands-only
and resources without a list cannot declare a deck. Manual `httpx.Command`
registrants must accurately declare their fields and `EmptyInputFault`.

The catalogue adds `presentation.deck` at version 2 and omits directions the
caller cannot run. An empty authorized set is `directions: {}`; a local skip
remains. Direction labels use `hints.<module>/<entity>.deck.<direction>.label`
and the declared English fallback. The registered queue and directions are
copied before projection. Existing picture, title, status and summary hints
still describe the card. No `resolved` or `undoable` key is produced: commands
do not yet expose an authoritative undo-token capability. An inverse command
does not imply undo.

A deck is a list-reading suggestion, not a command precondition, reservation or
security boundary. Tenant transactions, authorization and lifecycle rules
remain authoritative on the existing command routes. Client card rendering,
gestures, confirmation, pagination and local session order are separate work.

## Choices

A field's `Choices *ChoiceSet` declares semantics independently of `Widget` and
`Present`. Both appearance axes accept `tiles`, `chips`, `figure`, and `swatch`;
absent hints resolve to chips through `present.Resolve`. One choice is a string
(empty is `""`); many choices are an ordered, non-null string array. JSON null,
unknown IDs and duplicate IDs refuse without partial answers.

Compose `rest.Spec.Choices` or `rest.CommandOptions.Choices` with
`&entity.ChoiceFields{Fields: []entity.ChoiceField{...}}`. Each entry names an
existing field and supplies `Choices` and an optional compiled `Figure`.
`WithChoices` returns detached metadata and validates the whole overlay, including
defaults, Go shape, reserved names and conflicting enum/reference/status hints.
A standalone form passes the same resolved field through `forms.Field.Resolved`
and typed values through `forms.Model.ChoiceValues`.

`ChoiceSet` requires cardinality `one` or `many`, 1–256 options, and an explicit
empty policy. IDs match `[a-z][a-z0-9_-]{0,31}`; labels and group labels have at
most 160 Unicode characters without controls. `Empty` is `allow` with no message
or `refuse` with a sentence of at most 512 characters. A required struct tag
cannot contradict allowed empty. Search is enabled strictly above
`SearchThreshold`; zero means 12. Groups keep first appearance order, after
ungrouped options, with the exclusive option last.

An option picture is tagged `file` (canonical nonzero UUID), `composed`
(`entity.Picture` reference), or `colour` (`#RRGGBB`, canonicalized to uppercase).
Swatches require a colour on every ordinary option. An optional `NoPreference`
names an undecorated option that clears the others. `Maximum`, for many only,
uses a count within the ordinary options and behaviour `refuse` with a sentence,
or `replace-oldest` without one. Replacement retains the last N submitted IDs;
all IDs are validated before discarding any.

`Answer` validates a new selection. `Change(current, next)` also permits an
already-empty stored answer to remain empty, while refusing removal of its last
selection when required. `Apply` executes `pick`, `remove`, or `clear`; selecting
an existing member does not move its position. All results are detached and an
error returns no answer. These functions own no tenant or persistence rules.

Editable generated resources require a revision field. Their HTTP and form
updates require the observed revision, lock the current row, recheck tenant and
revision even for unchanged answers, then validate and write with the existing
outbox transaction. In-process helpers still require their caller to authorize
and provide its expected revision. Native forms submit repeated names, a presence
marker, and an optional JSON order marker. Presence without values clears; absence
leaves a patch untouched. New native selections append in display order; enhanced
forms retain interaction order. Native No preference clears other selections on
submit; JSON mixed exclusive/ordinary answers refuse.

A figure is a compiled value, not markup this package parses. `ChoiceFigure` is a
port: the picture owner's `CompileFigure` compiles the drawing against all ordinary
option IDs, and composition hands the result to `WithChoices`. Read-only figures
highlight selections without inputs. Editable figures have equivalent native
choices, keyboard focus movement and a selected-picture/count summary. Search,
interactive toggles and summary updates enhance an ordinary radio/checkbox form;
without JavaScript its summary is explicitly the last rendered selection.

### Composition

**Reused** — entity metadata, `present.Resolve`, REST row locks/revisions/outbox,
OptionChips, Media, and `ui.choice-figure`.
**Added** — finite ordered choice semantics and four presentation hints because
enums do not express pictures, groups, maxima, exclusivity or figure mappings.
**Made reusable** — detached choice policy, pure answer decisions, native form
transport, and a figure port shared by web and native shells.

### Limits

Products own names, artwork, storage and grants; no domain vocabulary is built in.
The catalogue document carries explicit choice and figure metadata, and a command's
resolved fields, at the version it already publishes: nothing an older shell could
misread was added to it, which is why its number did not move. Native rendering and
catalogue-version negotiation belong to the native shell. Unknown stored IDs remain visible for native correction rather than being
silently discarded or enhanced into a valid-looking selection.

## Sketch values

Declare `Drawing entity.Sketch` with `json:"drawing" required:"false" gorm:"type:jsonb"`.
Do not use JSON omission tags or a competing storage serializer. Reflection publishes
`type: sketch`; `*entity.Sketch` is also supported. Labels, help, visibility,
requiredness and immutability use the existing field contracts. Scalar hints,
defaults, sorting/filtering and image-cover hints are refused at mount.

A sketch is an array of strokes, each a nonempty array of `[x,y,ms]` int64 tuples.
Coordinates range from 0 to 1000 inclusive; elapsed milliseconds start at zero in
each stroke, never decrease, and cannot exceed 9007199254740991. The fixed limits
are 400 strokes and 20,000 total points. Decimal/exponent tokens, malformed tuples
and values beyond the limits are refused with `ErrInvalidSketch`. JSON and SQL
preserve stroke order, all integers, unset `null` and explicit empty `[]`.
`Scan` and `UnmarshalJSON` replace their receiver only on success. Assignment
aliases slices; copy them before independent editing or concurrent mutation.
`Sketch.Strokes` validates and returns a deep copy as primitive integer tuples,
preserving unset versus empty and returning no strokes for an invalid value.
It satisfies `components.SketchValue` without making components import entity.

`Sketch.Validate` is the common decision for the codec and owning commands.
`ValidateSketchFields` also checks containing fields marked required. CRUD checks
it before SQL; command preparation checks it before effects. Adopting commands
still own grant checks, request transactions, revision locking and outbox events.
There is no drawing table, permission or domain event in this value package.

### Composition

**Reused** — entity reflection, REST/CRUD validation, the resolved presentation
policy, forms, shared component styles and generated resource value slots.
**Added** — the bounded tuple codec, canvas editor and SVG reader, because scalar
lists and media references cannot represent a drawing's ordered strokes.
**Made reusable** — one value and its editor/reader for every declaring resource
or command, including custom screens composed through `ui/forms.Control`.

The kernel catalogue golden contains a reflected sketch field and command input.
Phone adoption follows with a parser that admits `sketch` and refreshed catalogue
provenance; existing phone builds with a closed type list reject it. Product
exposure must wait for that update. Recognition, scoring, signature validity,
server deadlines and drawing ownership remain product decisions.
