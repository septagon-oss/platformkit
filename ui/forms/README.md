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
Use `ui.Compose` for the shared stylesheet and `ui.Export` to capture this same
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

`screens.FormExample` forwards through deprecated `LegacyExample` to preserve its
historical HTML and IDs during migration. New compositions use `Example` because
legacy generated forms share IDs and cannot safely repeat on one page.
`Control` is available for custom trusted composition with an explicit control ID.

Run `go test ./ui/forms ./ui/screens ./ui/components` for local rendering, capture,
namespace and compatibility checks. Product HTTP/browser behavior remains the
responsibility of its assembled application checks.

`Example` refuses empty or duplicate field names with `ErrFields` before
rendering, because submitted values, labels and source identities need one owner.
