# Portable entity forms

`ui/forms` renders the existing field controls and captures their typed component
interfaces without a database, router, session or application. It belongs to the
public foundation and consumes [entity definitions](../../kit/entity/) and
[components](../components/). Use the Go version in the root `go.mod`.

Pass field definitions, explicit display labels and string values to `Example`:

```go
form, err := forms.Example("notes/new", forms.Model{
    Fields: []forms.Field{{
        Definition: entity.Field{Name: "title", Type: entity.TypeString, Required: true},
        Label: "Title",
    }},
    Values: map[string]string{"title": "Field notes"},
    Create: true,
}, forms.Options{
    Namespace: "notes-editor", Title: "New note",
    Action: "/notes", CancelURL: "/notes",
})
if err != nil {
    return err
}
return form.Node.Render(writer)
```

Import `github.com/septagon-oss/platformkit/ui/forms` and
`github.com/septagon-oss/platformkit/kit/entity`. `writer` is your `io.Writer`.
Rendering emits HTML; it does not open a connection or execute the save action.
Use `ui.Compose` for the shared stylesheet and `export.Export` to capture this same
example for design tools. The surrounding shell supplies assets and landmarks.

The source ID (`notes/new`) identifies an authoring occurrence. The separate DOM
namespace (`notes-editor`) identifies an instance and produces `notes-editor-form`.
Use distinct, stable namespaces when displaying two forms. Namespaces accept an
ASCII letter followed by letters, digits, underscores or hyphens. Field IDs encode
JSON names and retain identity when fields move; their labels and feedback refer
to those IDs. Invalid namespaces return `ErrNamespace` without a partial example.

`Field.Definition` is the existing entity metadata, not another schema. Labels and
select-option labels are presentation inputs; field names and submitted values
retain their domain meaning. Missing create values use declared defaults;
explicit empty values remain empty. Readonly fields never render. Immutable fields
are absent on create and disabled or readonly on edit. Handlers still enforce
these rules and decide validation errors, authorization and transaction outcomes.

The captured tree retains Core's Form, Input, Textarea, Select, Checkbox, Alert
and Button identities, including `field/<name>` children and the `actions` slot.
Edits are presentation candidates governed by existing export revision checks.
Rendering alone does not establish browser, native or A2UI adapter compatibility.
HTMX focus after a refused write requires the existing browser assets and the
handler's refusal response; direct rendering does not execute that controller.

`screens.FormExample` derives the form's DOM scope from the address it posts to
(`forms.Namespace`), so the create and edit screens of one entity own different
identities and two forms may share a page. `Control` is available for custom
trusted composition with an explicit control ID; without one it falls back to the
component's name-derived identity, which is why a page holding two forms built
that way collides — pass an ID when you compose more than one.

`MustExample` is `Example` for a composition already gated at mount. The generated
screens use it because a renderer returns a page, not an error, and `kit/rest`
already refuses a schema whose field names collide (`rest_test.go` names the
case). Reaching its panic means a resource reached a renderer unmounted.

Run `go test ./ui/forms ./ui/screens ./ui/components` for local rendering, capture,
namespace and compatibility checks. Product HTTP/browser behavior remains the
responsibility of its assembled application checks.

`Example` refuses empty or duplicate field names with `ErrFields` before
rendering, because submitted values, labels and source identities need one owner.

## The rich text field

A `widget:richtext` field draws its own editor's chrome — fixed toolbar with the
rest behind "More formatting", selection bubble, `/` command menu, link popover,
editing surface, `aria-live` save status, counter, draft-restore bar — and the
`<textarea name=…>` stays the submitted value, drawn first, with every piece of
chrome starting hidden. With no JavaScript the field is the textarea and the
"Formatting help" disclosure: the same form with less, not a control that lies
about working. `Field.Editor` carries what the browser may know and may not
guess — the record's address, the revision this form quotes (both absent when the
entity keeps no write count), the draft key, the image upload route, where a stored
picture's bytes are read back from, and whether the page shows the record's text or
a refused submission — and `ui/resource` fills them. The glue is
`ui/assets/js/richtext.js`, a module only a page with an editable richtext field
loads, mounting the vendored Tiptap bundle.

* **Reused** — `ui/components`' Button, Input, Textarea and Prose for every control, `ui/document`'s `head()` for script emission, `ui/hooks.go`'s declared-attribute vocabulary, `kit/richtext`'s schema and `Normalise` as the stored format, `modules/file`'s upload route, `components.js`' idiom of binding behaviour to drawn markup.
* **Added** — the chrome, because `ui/components` has no toolbar, menu, popover or progress component to borrow and inventing one is another task's share; `View.Modules`, because a page-weight budget is only real if the answer is per page; and the vendored bundle, because there was no vendor tree and 0069 §3 names the library.
* **Made reusable** — the `data-richtext-*` vocabulary and the `richtext:status` event, so a module's own screen can show the field's state in its own words; `richtextCommands`, one list the toolbar, the bubble and the menu cannot disagree about; and `scripts/richtext_bundle_build.sh`, which builds any future vendored bundle outside the tree.

## A phrase with a value in it

Every word this package draws a person reads is one message, and a value the
message interpolates — a keystroke, a field's label, a domain's refusal — arrives
as an argument, never joined in Go. `Options.Format` and `ControlProps.Format`
carry the request's `locale.Formatter`; with none composed, the source text
stands. Where both seams are composed the formatter answers, and a phrase that asks
for a value is never asked of the older one, because a provider that answers keys
with nothing to interpolate returns its copy with the verb unfulfilled — a finished
sentence in the language the package was written in is the better answer than a
broken one in the reader's. A note about the control a form chose is drawn as its
own sentence beside the field's own documentation, so two authors share one paragraph
without either sentence being assembled out of the other.

* **Reused** — `kit/locale`'s `Formatter` as the seam, `kit/locale/providers/xtext` as the provider that formats it, `ui/resource.Options.Text`'s existing argument-taking shape, `ui/components`' help paragraph and its `aria-describedby` pair.
* **Added** — `Format` on the two props structs, because widening `Text` to take arguments is a break on the frozen surface and the copy that needs a value cannot be assembled from a seam that carries none; `HelpNotes`/`HelperNotes` on the four help-bearing props structs, because that paragraph held exactly one string and a second sentence had nowhere to be drawn.
* **Made reusable** — within this package, one read path over both seams (`phrasesFor`), so every control and the form around it answer one phrase the same way. What a caller reuses is `Format` on the two props structs, `Control` for a screen built by hand, and the rule that catalogue copy is never an argument to catalogue copy, which is what keeps a wrapped phrase one wrapped phrase.

## Section steps

A `rest.Spec` or writable `rest.Singleton` opts into one-screen-per-section forms
through `Present.Forms.Create`, `.Update` or `.Input`. An absent form declaration
keeps its existing flat form. Record sections remain unchanged. Declare every
nonempty step's primary label; an unsectioned group comes first:

```go
Forms: &entity.FormsHints{Create: &entity.FormHints{
    Layout: "steps",
    Steps: []entity.FormStepHints{
        {Section: "", PrimaryLabel: "Next · {answer}", AnswerField: "title", EmptyLabel: "Enter a title"},
        {Section: "details", PrimaryLabel: "Next"},
        {Section: "options", PrimaryLabel: "Save note"},
    },
}},
```

Section keys refer to `Present.Sections` and fields' `ui:"section:…"` tags.
Membership and copy resolve through `kit/entity/present`; transport and renderers
consume that policy. `forms.StepExample` composes the existing Stepper and field
controls. Its input is a resolved form, position and encoded continuation.

Each Next/Back is a native POST to `/new/steps` or `/edit/steps`; the final button
posts to the resource's ordinary save address. Only the current step's controls
are rendered. Back retains raw answers without validation; Next validates that
step; final submission validates all fields and invokes the owning action.
Located errors select the earliest affected step and focus its first invalid
field. Other step errors name their step in the summary. JavaScript enhances
answer labels and focus; navigation and validation work without it.

Answers travel in a bounded, untrusted `__pkit_form` hidden value bound to the
form, mode, tenant, actor, record and structural schema digest. They survive Back
and reload of the submitted page. They are never authority, server drafts or
browser storage. Pages use no-store/no-referrer. Passwords and file controls
cannot opt in. Restarting the form starts again. Update steps require a persisted
server-owned revision; their original revision is checked by the final action.
For singletons, Load must return the stored root and Save must maintain its
revision and transactional events; the adapter locks that root before checking.

`rest.ReadForm[I,O]` declares typed read inputs without entity identity or storage.
Set the returned Spec's Module, Entity, Path, workspace Read guard and Present,
then mount it normally. I is a flat input struct; O is a readable struct or slice
of structs. `.Forms.Input` uses the same step policy; final submission alone calls
the read function with the current tenant transaction and actor. JSON clients
POST the same typed input at its API address. The callback must not mutate domain
state or emit events. Discovery exposes the input/output schemas and no CRUD
operations. Unsupported nested, map, password and file inputs are refused.

Copy uses the existing resource words under `form.<mode>.unsectioned.label` and
`form.<mode>.step.<key>.primaryLabel` / `.emptyLabel`, where a key is `unsectioned`
or `section:<key>`. Shared Back, Cancel, progress and Details words come from
`forms.Catalogues`. Copy changes do not change field IDs or continuation digests.

## Composition

**Reused** — `entity` field/section metadata, `present.Resolve`, `rest` parsing,
revision and transaction boundaries, `httpx` guards, and `ui/forms`, `ui/resource`
and `components.Stepper` own the shared form path.

**Added** — per-form layout declarations, resolved step membership, submitted-page
continuations and `rest.ReadForm`, because existing sections only grouped record
readings and no typed query could declare generated input pages.

**Made reusable** — one resolver and transition validator for stored forms and
typed reads, with a three-step HTTP/SQL/browser fixture.

## Limits and verification

No durable drafts, cross-device resume, branching, file staging, autosave or
idempotency promise is added. A reloaded final read can recompute its result;
uncertain writes retain the owning operation's recovery behavior. Products own
questions, cross-field domain rules, final labels and retention policy. Native
renderers can consume the resolved contract; no native renderer is implemented.

Run the focused suites with the development database:
`go test ./kit/entity/present ./kit/rest ./ui/forms ./ui/resource ./ui/screens`.
The browser fixture in `ui/screens` uses `PLATFORMKIT_BROWSER_TOOLS` pointing at an
external directory containing `@playwright/test` and `@axe-core/playwright`, with
`/usr/bin/chromium` available:
`go test ./ui/screens -run '^TestSteppedFormBrowserJourneys$' -count=1 -v`.
Set `PLATFORMKIT_DESIGN_GATE` to the design gate script to check each served step
and both error pages at 390 and 1440 pixels during the same run.

## Drawing fields

An `entity.Sketch` field uses the central `sketch` control and reading policy.
`Control` renders a labeled square canvas, Undo and Clear, one JSON successful
control and a static SVG fallback. Generated forms load `js/sketch.js` through
`document.View.Modules`; custom pages using `Control` must name that module too.
Serve it from the same `ui.Assets` tree as the stylesheet.

Pointer and keyboard input share one stroke value. Arrow keys move the cursor by
10 units, Shift+Arrow by one; Space/Enter starts or finishes, Escape/Tab cancels.
Undo removes the last stroke. Clear keeps one backup for Undo until another stroke
commits. Reset restores the originally rendered value. Canceled pointers and
removed fields discard tentative input. Submitting finishes the active stroke.
No action autosaves or writes browser storage.

Invalid drafts remain in the successful control until Clear deliberately replaces
them. Missing JavaScript/canvas preserves the saved drawing and explains the
limitation. Read-only fields show the shared `components.Sketch` reader. Its
required label is the accessible SVG name; layout supplies its display size.
Generated list thumbnails and record drawings use the same validated geometry.
The reader accepts `components.SketchValue`, implemented by `entity.Sketch`.
`Control` uses its supplied field label, then the declared label or field name;
generated screens supply the localized label through the existing form adapter.

**Reused** — field namespaces, native action buttons, shared design tokens, the
forms message catalogues and document asset lifecycle.
**Added** — stroke recording/history and SVG geometry; existing text and image
controls cannot edit or display this semantic value.
**Made reusable** — keyboard and pointer drawing through the same generated or
custom form control, with an SVG reader that needs no JavaScript.
