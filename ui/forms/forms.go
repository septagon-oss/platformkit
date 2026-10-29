// Package forms composes portable entity forms from detached presentation inputs.
// Handlers own authorization, parsing, persistence and the meaning of errors.
//
// Nothing here reads a request, a transaction or a catalogue: this package holds
// its own copy under messages/ and asks the formatter its caller composed to
// answer that copy in the language of the request, naming no provider, no tenant
// and no language. That is what the package gate holds to kit/entity, kit/locale,
// design, ui/icon, ui/css, ui/style, ui/components, ui/components/examples and
// gomponents.
package forms

import (
	"cmp"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	"github.com/septagon-oss/platformkit/ui/style"
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

func immutableNote(immutable bool) string {
	if immutable {
		return "Changed by a command of its own, not by this form."
	}
	return ""
}

// Field presents an existing entity definition. Label and Options supply display
// words explicitly; they do not change the definition's names or enum values.
// Empty labels use the field name, and absent options use the declared enum text.
type Field struct {
	Resolved                      *present.ResolvedField
	PictureSource, ComposedSource string
	// Anchor names this composed control; empty keeps standalone forms unanchored.
	Anchor     screen.AnchorRef
	Definition entity.Field
	Label      string
	// Help is the line under the control: what resource.Words.FieldHelp answers for
	// the field — the author's declared `help:` when they wrote one, in the
	// request's language, the field's Doc when they did not. Empty falls back to
	// Doc, so a caller that projects no reading words at the control (a design
	// export, a test) reads what it always read. The label beside it and this line
	// are the same request's two words: a caller that translates one translates
	// both.
	Help    string
	Options []components.SelectOption
	// Editor carries what a widget:richtext field's chrome needs beyond the
	// field: the record's own address and the revision this form quotes, the key
	// an unsaved draft is kept under, the route an image is uploaded to and the
	// deployment's stated ceiling for one.
	//
	// Each is decided by whoever draws the form — ui/resource for a generated
	// screen — and each is absent rather than guessed when it cannot be known:
	// no Endpoint means no autosave and a draft kept on this device instead, no
	// DraftKey means nothing is kept at all (a new record has no key to be kept
	// under), no FileUpload means the session may not upload and the image entry
	// is not drawn. A control the browser assembled out of guesses is a control
	// that fails at the server instead of where the person can see why.
	Editor Editor
}

// Editor is the rich text field's server-side answer to "what may this browser
// do, and where". Empty fields mean the feature is off for this field; the glue
// reads the attributes this data is drawn into and never invents a URL, a key or
// a permission of its own.
type Editor struct {
	// Endpoint is the record's own REST address, and ETag the quoted revision this
	// form was drawn from. Both empty means the editor saves nothing as the author
	// writes and keeps an unsaved draft instead. A form that posts the field itself
	// leaves them empty: its own save is then the row's only writer.
	Endpoint, ETag string
	// DraftKey is where an unsaved draft is kept in this browser, composed here
	// from the tenant, the resource, the record and the field — never by the page.
	DraftKey string
	// FileUpload is the route an image is posted to, and Limits the sentence that
	// states the formats and the ceiling before any refusal.
	FileUpload, Limits string
	// ImageSrc is where one stored file's bytes are read back, with {file} standing
	// for the id an upload returned: the address the editor draws a picture from
	// while the document keeps the pk-file reference it posts. Empty means no
	// preview is drawn and a reference shows as it was written.
	ImageSrc string
	// Submitted says this render is the answer to a write the server refused, so its
	// controls hold what the person posted rather than what the record holds. The
	// browser keeps an unsaved draft through such a page: an echo of a refused
	// submission is no evidence that anything was saved.
	Submitted bool
}

// Model contains a form's already-read values and already-decided field errors.
// A missing value uses the declared default only on create; an explicit empty
// value remains empty. ReadOnly fields are omitted, as are immutable create fields.
type Model struct {
	ChoiceValues map[string][]string
	Fields       []Field
	Values       map[string]string
	Errors       map[string]string
	Immutable    []string
	Detail       string
	Create       bool
}

// Options supplies presentation and command addresses. Namespace is a DOM scope,
// separate from an example's source identity. Use a unique stable namespace per
// rendered instance. It must start with an ASCII letter and contain only letters,
// digits, underscores or hyphens. The resulting form ID is Namespace + "-form".
type Options struct {
	// View names this composed form, independently of its DOM namespace.
	View                                   screen.ViewRef
	Namespace                              string
	Action, CancelURL, Title               string
	CancelLabel, SubmitLabel, FailureTitle string
	// DetailTone is how the refused line is announced: "" for a fault, drawn and read
	// as one, or the quiet tone of an outcome the application reached by working as
	// designed — a feature this workspace's plan does not carry — which nothing broke
	// and no alarm is owed. It says nothing where Model.Detail is empty, because no
	// notice is drawn then.
	DetailTone string
	// Format answers this form's chrome in the request's language: a catalogue
	// key, the readable source text to use when no catalogue holds it, and the
	// values the phrase interpolates. It is the kernel's own formatter, not a
	// signature this package invented, so the provider keeps ownership of
	// interpolation, plural rules and regional number format. Nil — the default —
	// answers every phrase with its source text.
	Format locale.Formatter
	// Text answers one phrase that carries no value. Format answers every phrase,
	// and Format is asked first whenever both are composed.
	//
	// Deprecated: a phrase that asks for a value is answered from its source text
	// and never asked of Text, so this seam answers only the phrases carrying none.
	// Compose Format instead.
	Text func(key, fallback string) string
}

// phrases is this form's chrome, asked of the formatter the render composed.
func (o Options) phrases() phraser { return phrasesFor(o.Format, o.Text) }

// ErrNamespace reports an unusable instance scope before any example is returned.
var ErrNamespace = errors.New("forms: namespace must start with a letter and contain only letters, digits, underscores or hyphens")

// ErrFields refuses ambiguous input names and the DOM/source identities they create.
var ErrFields = errors.New("forms: field names must be nonempty and unique")

// Namespace derives a form's DOM scope from the address it posts to. It exists
// because the two things a generated form's identity has to satisfy pull apart:
// it must differ from every other form on the page, and it must not change when
// the same screen re-renders a refused submission — the swap that puts the field
// errors back targets the id the form was drawn with. An address gives both: one
// screen posts to one address, and a refused POST is answered by the screen at
// that same address. A fixed id gave neither, which is why one screen could hold
// only one generated form.
//
// The answer is always something Example accepts: a letter, a digit, an
// underscore or a hyphen is kept, everything else is a separator, consecutive
// separators collapse and a leading separator is dropped, so the identity stays
// readable in DevTools rather than a run of dashes. Two addresses are given the
// same identity only where they differ in punctuation alone, and a screen serves
// one address, so that never puts two forms on one page. A leading digit is
// prefixed rather than dropped, and an input with nothing usable in it gets a
// fixed scope. The tests feed it the inputs that would break that and
// checks every byte of every answer.
func Namespace(action string) string {
	var b strings.Builder
	for i := 0; i < len(action); i++ {
		c := action[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			b.WriteByte(c)
			continue
		}
		// One separator for a run of them, and none at the front.
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "pk-form"
	}
	if !letter(out[0]) {
		return "pk-" + out
	}
	return out
}

// Example captures the same typed Form and field constructors used at runtime.
// It returns a presentation candidate; rendering or editing it performs no write.
// DOM identities and HTMX targets belong to Namespace, never to the source ID.
func Example(id string, model Model, options Options) (examples.Example, error) {
	if !validNamespace(options.Namespace) {
		return examples.Example{}, ErrNamespace
	}
	seen := make(map[string]bool, len(model.Fields))
	for _, field := range model.Fields {
		if err := validateChoiceField(field); err != nil {
			return examples.Example{}, err
		}
		name := field.Definition.Name
		// The name is in the error because it is the thing to fix: one field with no
		// name has no label to write, and two of one name draw two controls the
		// browser cannot tell apart.
		if name == "" {
			return examples.Example{}, fmt.Errorf("%w: a field has no name to label or report on", ErrFields)
		}
		if seen[name] {
			return examples.Example{}, fmt.Errorf("%w: two fields are named %q", ErrFields, name)
		}
		seen[name] = true
	}
	return example(id, model, options), nil
}

// MustExample is Example for a composition whose field names were already kept
// distinct by the thing that built the schema. It exists for the generated
// screens, which render a page and have no channel left to report on: kit/crud
// derives one name per struct field, falling back to the Go name when the JSON
// tag is empty, and go vet refuses a struct that repeats a tag, so a schema that
// reached a mounted route cannot hold two fields of one name.
//
// Reaching the panic therefore means a hand-built entity.Schema — a design
// invocation, an adapter — named two fields the same. That is a composition bug,
// and it is said in the same shape regexp.MustCompile and this repository's own
// component constructors already use it.
func MustExample(id string, model Model, options Options) examples.Example {
	example, err := Example(id, model, options)
	if err != nil {
		panic(err.Error() + " — a schema reaching a renderer should have been refused where it was built")
	}
	return example
}

func example(id string, model Model, options Options) examples.Example {
	formID := options.Namespace + "-form"
	word := options.phrases()
	body := []g.Node{}
	if model.Detail != "" {
		body = append(body, examples.ExampleWithSlots(examples.ExampleInfo{ID: "error", ComponentID: "pk-ui.component.alert"},
			components.AlertProps{Tone: cmp.Or(options.DetailTone, "danger"),
				Title:   cmp.Or(options.FailureTitle, word("forms.form.failureTitle", "That could not be saved")),
				Message: model.Detail, Bordered: true},
			components.AlertSlots{}, components.AlertWithSlots).Node)
		var links []g.Node
		richtextError := false
		for _, field := range model.Fields {
			if message := model.Errors[field.Definition.Name]; message != "" {
				richtextError = richtextError || field.Definition.Widget == "richtext"
				id := options.Namespace + "-field-" + hex.EncodeToString([]byte(field.Definition.Name))
				links = append(links, h.Li(h.A(h.Href("#"+id),
					g.Text(word("forms.form.fieldError", "%[1]s: %[2]s", cmp.Or(field.Label, field.Definition.Name), message)))))
			}
		}
		if len(links) > 0 && richtextError {
			body = append(body, h.Nav(g.Attr("aria-label", word("forms.form.fieldsToCorrect", "Fields to correct")), h.Ul(links...)))
		}
	}
	for _, field := range model.Fields {
		f := field.Definition
		immutable := slices.Contains(model.Immutable, f.Name)
		// Nothing the server owns renders a control. Base's four because a form
		// cannot name them, and the row's own write count because a control that
		// posted back the number it rendered would move the row to that number —
		// which is the one thing the stale-write refusal is built on.
		if entity.ServerOwned(f) || (model.Create && immutable) {
			continue
		}
		value, present := model.Values[f.Name]
		if !present && model.Create {
			value = f.Default
		}
		body = append(body, Control(ControlProps{Field: field,
			// Hex preserves arbitrary JSON names without collisions with control
			// error/help suffixes, while reordering leaves field identity intact.
			ID:           options.Namespace + "-field-" + hex.EncodeToString([]byte(f.Name)),
			ChoiceValues: choiceModelValues(model, f), Value: value, Error: model.Errors[f.Name], Immutable: immutable, Format: options.Format, Text: options.Text}))
	}
	body = append(body, examples.ExampleWithChildren(
		examples.ExampleInfo{ID: "actions", ComponentID: "pk-ui.component.formactions"}, components.FormActionsProps{}, []g.Node{
			examples.ExampleWithSlots(examples.ExampleInfo{ID: "cancel", ComponentID: "pk-ui.component.button"},
				components.ButtonProps{ComponentProps: components.ComponentProps{Attrs: actionAttrs(options.View, "cancel")},
					Label:   cmp.Or(options.CancelLabel, word("forms.form.cancel", "Cancel")),
					Variant: "secondary", Href: options.CancelURL}, components.ButtonSlots{}, components.ButtonWithSlots).Node,
			examples.ExampleWithSlots(examples.ExampleInfo{ID: "save", ComponentID: "pk-ui.component.button"},
				components.ButtonProps{ComponentProps: components.ComponentProps{Attrs: actionAttrs(options.View, "submit")},
					Label: cmp.Or(options.SubmitLabel, word("forms.form.submit", "Save")),
					Type:  "submit"}, components.ButtonSlots{}, components.ButtonWithSlots).Node,
		}, components.FormActions).Node)
	// A file control submits a body a urlencoded form cannot carry, and the
	// encoding belongs to the form rather than to whoever composed it: one
	// field that needs it is enough to break the whole submission. It is added
	// here and not to FormProps because nothing else may decide it — a page
	// that sets enctype by hand and renders no file control is a page that
	// stopped validating its own fields.
	props := components.FormProps{
		ComponentProps: components.ComponentProps{ID: formID, Class: style.New().Width(style.SFull).MaxWScaled(style.MaxWSM).Compile()},
		HTMXProps: components.HTMXProps{
			Post: options.Action, Target: "#" + formID, Swap: "outerHTML", Select: "#" + formID},
		Action: options.Action, Label: options.Title,
	}
	if rendersFile(model) {
		props.Attrs = map[string]string{"enctype": "multipart/form-data"}
	}
	if options.View != "" {
		if props.Attrs == nil {
			props.Attrs = map[string]string{}
		}
		props.Attrs["data-screen-view"] = string(options.View)
	}
	return examples.ExampleWithChildren(
		examples.ExampleInfo{ID: id, ComponentID: "pk-ui.component.form", Group: "Screens", Name: options.Title}, props,
		body, components.Form)
}

// rendersFile reports whether the form draws a control that submits a file.
// It applies the same skip the body loop applies — a read-only field, or an
// immutable one on create, renders no control and so needs no encoding.
func rendersFile(model Model) bool {
	for _, field := range model.Fields {
		f := field.Definition
		if entity.ServerOwned(f) || (model.Create && slices.Contains(model.Immutable, f.Name)) {
			continue
		}
		if f.Widget == "file" {
			return true
		}
	}
	return false
}

func validNamespace(value string) bool {
	if value == "" || !letter(value[0]) {
		return false
	}
	for i := 1; i < len(value); i++ {
		c := value[i]
		if !letter(c) && (c < '0' || c > '9') && c != '-' && c != '_' {
			return false
		}
	}
	return true
}

func letter(c byte) bool { return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') }
