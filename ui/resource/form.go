package resource

import (
	"slices"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/display"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	"github.com/septagon-oss/platformkit/ui/document"
	"github.com/septagon-oss/platformkit/ui/forms"
)

// Form is the create and edit screen: one control per writable field, derived
// from the field's type and its ui:"widget:" tag, with the fields a command
// owns shown read-only so a person can see them and not change them here. errs
// and detail are a refused write's, and a form carrying a detail is a 422: the
// kernel rolls the transaction back past 400, and htmx swaps a 422 in place
// rather than treating it as an error nobody sees.
func Form(r Resource, o Options, action, title string, row map[string]any, errs map[string]string, detail string, create bool) document.View {
	at := r.Screen
	status := 0
	if detail != "" {
		status = statusUnprocessableEntity
	}
	return document.View{Title: title, Status: status, Modules: richtextModules(r), Body: []g.Node{
		breadcrumb(o, display.Humanize(r.reading(o).Plural), at, title),
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
	for _, f := range readable(r.Schema.Fields) {
		if f.Widget == "richtext" && !slices.Contains(r.Immutable, f.Name) {
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

// richtextEditor is the server's answer to "what may this field's editor do, and
// where". An author who is writing has no record yet, so there is nothing to keep
// a draft under: the draft key appears with the record's id and nowhere else. No
// write address is offered beside it, because this form is the write: it posts the
// field and quotes the row's own write count as the condition on that post. A
// browser handed the same address would PATCH the field as the person types, move
// the row off the revision the form still carries, and refuse the Save pressed a
// moment later — keeping no draft in which unsaved words would then live.
//
// submitted says this render is the answer to a write that was refused: its
// controls carry the person's own post rather than the record's text. Nothing
// compares a kept draft against that text — a refused write saves nothing, and the
// copy on the device is the only place unsaved words live.
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

// FormExample captures the same form body that Form serves, without its page
// chrome. Supply an owner-local example ID and synthetic row/error inputs for
// design export. Children retain Core's typed contracts and named slots; fields
// use "field/" plus their schema name so actions and errors cannot shadow them.
// Property edits produce presentation candidates, not schema or database writes.
// Native editor support and interactive flow behavior require separate evidence.
//
// The form's DOM scope comes from action, not from id: two screens of one entity
// (create and edit) post to different addresses and so own different scopes, and
// the refused POST that swaps errors back in is answered at the address the form
// was drawn from, so the swap still finds its target.
func FormExample(id string, r Resource, o Options, action, title string, row map[string]any, errs map[string]string, detail string, create bool) examples.Example {
	// The form asks for every field its author did not keep off every screen:
	// `hidden` is the one visibility off this screen too, because a control for a
	// field no screen may name is a question nobody may answer, and `detail` stays
	// on it — a required field a form cannot ask for is a record nobody can
	// create. See readable. The one field added after this loop is the edit form's
	// own write condition, below, so the capacity holds room for it.
	fields := make([]forms.Field, 0, len(r.Schema.Fields)+1)
	words := o.Words(r.Schema)
	// A render carrying a refusal line is the form redrawn over a write that was
	// refused, with the person's own words in its controls.
	refused := detail != ""
	for _, field := range readable(r.Schema.Fields) {
		f := formField(words, field)
		if f.Definition.Widget == "richtext" {
			f.Editor = richtextEditor(r, row, create, f.Definition.Name, refused)
		}
		fields = append(fields, f)
	}
	values := make(map[string]string, len(row))
	for name, value := range row {
		values[name] = display.Text(value)
	}
	// An edit carries the row's write count back as a hidden value, so the write it
	// posts can be refused against a row that moved since the person read it. It is a
	// hidden value and not a control for two reasons that are one: nobody may type the
	// number of a row's writes (kit/rest refuses a body that names the field), and a
	// form whose hidden machinery is drawn as a control is a form that invites people to
	// break its own safety. A create quotes nothing: there is no row to have read.
	//
	// The value is the number as the row has it, which is also what a refused write gives
	// back — the person who is told their deadline is invalid resubmits against the
	// revision they were already quoting, not against one they never saw.
	if condition, quoted := revisionCondition(r, row, create); quoted {
		fields = append(fields, forms.Field{Definition: entity.Field{
			Name: entity.RevisionCondition, Type: entity.TypeString, Widget: "hidden",
			Doc: "The revision this form was drawn from. The write is refused if the row has moved.",
		}})
		values[entity.RevisionCondition] = condition
	}
	return forms.MustExample(id, forms.Model{Fields: fields, Values: values, Errors: errs,
		Immutable: r.Immutable, Detail: detail, Create: create}, forms.Options{
		// The address the form posts to is its DOM scope: see forms.Namespace.
		Namespace: forms.Namespace(action),
		Action:    action, CancelURL: r.Screen, Title: title,
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

// refusalHeading and refusalTone are the two parts of the verdict a form draws, read
// from a render that named none as the empty pair they were drawn from before.
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

// revisionCondition is the edit form's condition on the write: the row's own write count,
// and whether this form may quote one at all. A resource whose entity keeps no count,
// and a form that is not editing a row it read, quote nothing — and the door they post to
// writes as plainly as it always did. A singleton is the second case by declaration: its
// API is a PUT with no tag to quote (kit/rest carries If-Match on the patch and the delete
// alone), so its screen carries a condition its route would not honour.
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
		// A row that arrived with a revision no field of this schema declares is not this
		// renderer's to speak for; it quotes nothing, and the write is the plain one.
		return "", false
	}
	if count, has := row[entity.RevisionField]; has {
		return display.Text(count), true
	}
	if carried, has := row[entity.RevisionCondition]; has {
		return display.Text(carried), true
	}
	return "", false
}

// Control adapts an entity field to the portable form control, preserving the
// generated screens' existing labels, choices and name-derived DOM identity.
func Control(f entity.Field, value, fieldErr string, immutable bool) g.Node {
	return forms.Control(forms.ControlProps{Field: formField(Words{}, f), Value: value, Error: fieldErr, Immutable: immutable})
}

// formField projects display words at the screen boundary. The portable form
// owns controls; the value words keep their owner in kit/entity/display, and
// `words` is what puts them in the request's language — the zero Words reads
// each of them exactly as written.
func formField(words Words, f entity.Field) forms.Field {
	options := make([]components.SelectOption, 0, len(f.Enum))
	for _, value := range f.Enum {
		options = append(options, components.SelectOption{Label: words.EnumWord(f, value), Value: value})
	}
	return forms.Field{Definition: f, Label: words.FieldLabel(f), Help: words.FieldHelp(f), Options: options}
}

// table is the list screen's rows: the field a row is known by first, as the
// link into it, then every other field the schema does not hide.
//
