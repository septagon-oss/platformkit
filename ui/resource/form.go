package resource

import (
	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/display"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	"github.com/septagon-oss/platformkit/ui/document"
	"github.com/septagon-oss/platformkit/ui/forms"
)

// Form renders the resolved create or update layout with submitted values.
// Nonempty detail marks a refused write and returns status 422; errs names
// individual controls. Invalid supported policy returns an HTML render error.
func Form(r Resource, o Options, action, title string, row map[string]any, errs map[string]string, detail string, create bool) document.View {
	r = r.policy(o)
	if err := r.validatePolicy(); err != nil {
		return document.View{Body: []g.Node{invalid(err)}}
	}
	at := r.Screen
	status := 0
	if detail != "" {
		status = statusUnprocessableEntity
	}
	return document.View{Title: title, Language: r.Resolved.Language, Status: status, Modules: formModules(r), Body: []g.Node{
		breadcrumb(o, r.collectionName(), at, title),
		components.Toolbar(components.ToolbarProps{Title: title}),
		FormExample(at, r, o, action, title, row, errs, detail, create).Node,
	}}
}

// richtextModule is the editor's browser half, as a path under the shell's asset
// prefix. It is a module and not a controller: a page that draws no rich text
// field carries no tag for it and downloads none of it.
const richtextModule = "js/richtext.js"

// richtextModule asks whether this form draws an editor at all: an editable rich
// text field, which is the only thing that needs 470 KB of editor. A record whose
// rich text is immutable is read-only chrome and no editor, and the page that
// draws one costs the same as the page that draws none.
func richtextModules(r Resource) []string {
	for _, f := range r.Resolved.Fields {
		if f.Control == "richtext" && (f.Editable.Create || f.Editable.Update) {
			return []string{richtextModule}
		}
	}
	return nil
}

// fileUploadRoute is the file module's own upload address on the workspace
// surface, spelled rather than derived: the editor posts the bytes it is handed to
// this route and to no other, and never builds a path out of a record's own.
const fileUploadRoute = "/api/v1/file/files"

// fileImageSrc is the same module's own address for reading one stored file back,
// with {file} standing for the id an upload answered with. The editor substitutes
// an id an upload handed it and no other text, so the picture a person just
// uploaded is a picture they can look at while the document, the posted value and
// the line the server validates all keep the pk-file reference they always held.
// Spelled here beside the upload route rather than built from it, for the same
// reason: the browser is handed addresses, never a rule for making one.
const fileImageSrc = "/api/v1/file/files/{file}/content"

// richtextEditor scopes an existing record draft without enabling autosave.
// submitted preserves unsaved text after a refused write; create has no draft key.
func richtextEditor(r Resource, row map[string]any, create bool, name string, submitted bool) forms.Editor {
	editor := forms.Editor{FileUpload: fileUploadRoute, ImageSrc: fileImageSrc, Submitted: submitted}
	if create || row == nil || r.Singleton || r.API == "" {
		return editor
	}
	id, _ := row["id"].(string)
	if id == "" || !r.offers("update") {
		return editor
	}
	editor.DraftKey = r.API + "/" + id + "/" + name
	return editor
}

// statusUnprocessableEntity is the one status this package writes: the 422 a
// refused form answers with. It is spelled here rather than read from net/http
// because a renderer of values links no HTTP server.
const statusUnprocessableEntity = 422

// FormExample captures the same form body as Form for offline design export.
// id names the example; action scopes control identities across instances and
// refusal swaps. Inputs are synthetic values, not persistence operations.
func FormExample(id string, r Resource, o Options, action, title string, row map[string]any, errs map[string]string, detail string, create bool) examples.Example {
	kind := "edit"
	if create {
		kind = "create"
	}
	r = r.policy(o)
	if err := r.validatePolicy(); err != nil {
		return examples.Example{Node: invalid(err)}
	}
	resolved := r.resolvedView(kind)
	// The form asks in the order its published view names, so the control it draws
	// and the anchor the screen declared are one decision. A field's own words come
	// from the resolved policy, which names its appearance. The one field this loop
	// does not draw is the edit form's own write condition.
	fields := make([]forms.Field, 0, len(resolved.Controls)+1)
	immutable := []string{}
	choiceValues := map[string][]string{}
	words := o.Words(r.Schema)
	for _, control := range resolved.Controls {
		if control.Name != "field" {
			continue
		}
		// A map argument's key is a control of its own, drawn from its own
		// declaration; a plain field is drawn from the policy the caller resolved.
		var f forms.Field
		if len(control.Path) > 1 {
			f = formField(words, controlField(r.Schema, control), o)
		} else {
			field, _ := entity.FieldNamed(r.Schema.Fields, control.Path[0])
			f = resolvedFormField(field, r.field(control.Path[0]), o)
		}
		if ref := r.ref(kind, ""); ref != "" {
			f.Anchor, _ = ref.Anchor("field", control.Path...)
		}
		if !control.Editable {
			immutable = append(immutable, control.Path[0])
		}
		// A choice answers from the row's own selection, or from the refused
		// write's, which arrives here in the same row map.
		field := f.Definition
		if field.Choices != nil {
			if value, exists := row[field.Name]; exists {
				choiceValues[field.Name] = readChoiceIDs(field, value)
			}
		}
		if f.Definition.Widget == "richtext" {
			f.Editor = richtextEditor(r, row, create, control.Path[0], detail != "")
		}
		fields = append(fields, f)
	}
	values := make(map[string]string, len(row))
	for name, value := range row {
		values[name] = formValueString(r.Schema.Fields, name, value)
	}
	// An edit carries the observed revision, including after a refused write.
	// A create has no observed row and quotes nothing.
	if condition, quoted := revisionCondition(r, row, create); quoted {
		fields = append(fields, forms.Field{Definition: entity.Field{
			Name: entity.RevisionCondition, Type: entity.TypeString, Widget: "hidden",
			Doc: "The revision this form was drawn from. The write is refused if the row has moved.",
		}})
		values[entity.RevisionCondition] = condition
	}
	return forms.MustExample(id, forms.Model{ChoiceValues: choiceValues, Fields: fields, Values: values, Errors: errs,
		Immutable: immutable, Detail: detail, Create: create}, forms.Options{
		// The address the form posts to is its DOM scope: see forms.Namespace.
		Namespace: forms.Namespace(action),
		View:      r.ref(kind, ""),
		Action:    action, CancelURL: r.Screen, Title: title,
		// The request's formatter answers the chrome, so a phrase with a value in it
		// — a keystroke inside a toolbar name, a label inside a placeholder — is one
		// translated sentence rather than two English halves joined in Go. The guard
		// stays here because a nil locale in an interface is not nil to the code that
		// asks it: a render with no composed locale composes no formatter instead.
		Format: chromeFormatter(o.Locale),
		Text: func(key, fallback string) string {
			if o.Locale == nil {
				return fallback
			}
			return o.Locale.Text(key, fallback)
		},
		// The verdict's own name and tone, from the caller that looked them up. A render
		// that names none keeps "That could not be saved" and the fault tone it kept
		// before either field existed.
		FailureTitle: refusalHeading(o), DetailTone: refusalTone(o),
	})
}

// refusalHeading keeps the form's default heading when no refusal was supplied.
func refusalHeading(o Options) string {
	if o.Refusal == nil {
		return ""
	}
	return o.Refusal.Heading
}

func refusalTone(o Options) string {
	if o.Refusal == nil {
		return ""
	}
	return o.Refusal.Tone
}

// revisionCondition quotes a declared revision from a read or refused form.
// Creates and singleton PUTs have no supported revision condition.
func revisionCondition(r Resource, row map[string]any, create bool) (string, bool) {
	if create || r.Singleton || row == nil {
		return "", false
	}
	counts := false
	for _, f := range r.Schema.Fields {
		if f.Name == entity.RevisionField && f.Type == entity.TypeInt {
			counts = true
		}
	}
	if !counts {
		return "", false
	}
	if count, has := row[entity.RevisionField]; has {
		return scalarText(count), true
	}
	if carried, has := row[entity.RevisionCondition]; has {
		return display.Text(carried), true
	}
	return "", false
}

// Control adapts an entity field to the portable form control, preserving the
// generated screens' existing labels, choices and name-derived DOM identity.
func Control(f entity.Field, value, fieldErr string, immutable bool) g.Node {
	return forms.Control(forms.ControlProps{Field: formField(Words{}, f, Options{}), Value: value, Error: fieldErr, Immutable: immutable})
}

// formField resolves a standalone field for compatibility controls and commands.
func formField(words Words, f entity.Field, o Options) forms.Field {
	return resolvedFormField(f, words.field(f), o)
}
