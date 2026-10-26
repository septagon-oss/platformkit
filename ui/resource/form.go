package resource

import (
	"cmp"
	"slices"

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
	return document.View{Title: title, Language: r.Resolved.Language, Status: status, Modules: richtextModules(r), Body: []g.Node{
		breadcrumb(o, r.collectionName(o), at, title),
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
	r = r.policy(o)
	if err := r.validatePolicy(); err != nil {
		return examples.Example{Node: invalid(err)}
	}
	fields := make([]forms.Field, 0, len(r.Resolved.Fields)+1)
	immutable := []string{}
	choiceValues := map[string][]string{}
	names := r.Resolved.Layout.Update
	if create {
		names = r.Resolved.Layout.Create
	}
	for _, name := range names {
		field, _ := fieldFor(r.Schema.Fields, name)
		f := resolvedFormField(field, r.field(name), o)
		// A choice answers from the row's own selection, or from the refused
		// write's, which arrives here in the same row map.
		if field.Choices != nil {
			if value, exists := row[field.Name]; exists {
				choiceValues[field.Name] = readChoiceIDs(field, value)
			}
		}
		if f.Definition.Widget == "richtext" {
			f.Editor = richtextEditor(r, row, create, name, detail != "")
		}
		fields = append(fields, f)
	}
	if !create {
		for _, field := range r.Resolved.Fields {
			f, _ := fieldFor(r.Schema.Fields, field.Name)
			if field.OnRecord && !field.Editable.Update && !entity.ServerOwned(f) && !slices.Contains(names, field.Name) {
				immutable = append(immutable, field.Name)
				fields = append(fields, resolvedFormField(f, field, o))
			}
		}
	}
	values := make(map[string]string, len(row))
	for name, value := range row {
		values[name] = scalarText(value)
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
		Action:    action, CancelURL: r.Screen, Title: title,
		// The two buttons and the empty option of a select are this package's words to
		// give, because the form builder one layer below the catalogue prints its own
		// English when nobody hands it theirs: a Portuguese form whose every field the
		// module translated would otherwise report its refused save and its own buttons
		// in English under a Portuguese heading.
		ChooseLabel: chooseWord(o.Words(r.Schema)),
		CancelLabel: o.Label("screens.cancel", "Cancel"),
		SubmitLabel: o.Label("screens.save", "Save"),
		Notes:       o.notes(),
		Text: func(key, fallback string) string {
			if o.Locale == nil {
				return fallback
			}
			return o.Locale.Text(key, fallback)
		},
		// The verdict's own name and tone, from the caller that looked them up. A render
		// that names none keeps "That could not be saved" and the fault tone it kept
		// before either field existed.
		// The heading a refusal is drawn under is the verdict's own name where the caller
		// looked one up, and this catalogue's words for a failed save where it named
		// none. Either way it is read in the request's language.
		FailureTitle: cmp.Or(refusalHeading(o), o.Label("screens.failure_title", "")),
		DetailTone:   refusalTone(o),
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
	return controlIn("", Words{}, f, value, fieldErr, immutable)
}

// Control is one field's control as this screen would draw it inside a form: the
// field's own label, help and enum words, and the empty option's sentence. A caller
// that draws a single control outside a whole screen — a command's argument, a filter
// above a table — asks this rather than assembling forms.Control itself, which is how
// a second vocabulary of untranslated controls starts.
func (o Options) Control(f entity.Field, value, fieldErr string, immutable bool) g.Node {
	return controlIn("", o.words(), f, value, fieldErr, immutable)
}

// controlIn draws one control under a DOM identity the caller owns — a command's
// argument, whose id names the action and the locale so two commands of one verb on
// one page stay two controls — with the words the request is read in. Empty id leaves
// the identity to the form, which is what a control drawn inside a generated screen
// asks. Both routes build their props here, so a control cannot be addressed by one
// caller and left untranslated by another.
func controlIn(id string, words Words, f entity.Field, value, fieldErr string, immutable bool) g.Node {
	return forms.Control(forms.ControlProps{ID: id, Field: formField(words, f, Options{}), Value: value,
		Error: fieldErr, Immutable: immutable, ChooseLabel: chooseWord(words), Notes: words.notes()})
}

// chooseWord is the empty option's own sentence — the row a reader picks to take a
// choice back — addressed outside any one resource, because every select of every
// entity shows it: `screens.choose`, the prefix the New, Edit and Save buttons live
// under.
func chooseWord(words Words) string {
	if words.text == nil {
		return "Choose…"
	}
	return words.text("screens.choose", "Choose…")
}

// notes are ui/forms' three sentences about a control — why a value is read-only
// here, that an identifier has no picker, how a list is typed in — in the language
// these words are read in. The fallback is empty on purpose: with no seam this is the
// form layer's own English, which is what a form without a catalogue has always said.
func (w Words) notes() forms.Notes {
	if w.text == nil {
		return forms.Notes{}
	}
	return forms.Notes{
		Immutable:      w.text("screens.immutable_note", ""),
		RelatedRecord:  w.text("screens.related_record", ""),
		CommaSeparated: w.text("screens.comma_separated", ""),
	}
}

// notes are the same three sentences for a caller holding the request's options.
func (o Options) notes() forms.Notes {
	return forms.Notes{
		Immutable:      o.Label("screens.immutable_note", ""),
		RelatedRecord:  o.Label("screens.related_record", ""),
		CommaSeparated: o.Label("screens.comma_separated", ""),
	}
}

// formField resolves a standalone field for compatibility controls and commands.
func formField(words Words, f entity.Field, o Options) forms.Field {
	return resolvedFormField(f, words.field(f), o)
}
