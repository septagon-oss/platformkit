// Package forms composes portable entity forms from detached presentation inputs.
// Handlers own authorization, parsing, persistence and the meaning of errors.
package forms

import (
	"cmp"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	g "maragu.dev/gomponents"
)

// Field presents an existing entity definition. Label and Options supply display
// words explicitly; they do not change the definition's names or enum values.
// Empty labels use the field name, and absent options use the declared enum text.
type Field struct {
	Definition entity.Field
	Label      string
	Options    []components.SelectOption
}

// Model contains a form's already-read values and already-decided field errors.
// A missing value uses the declared default only on create; an explicit empty
// value remains empty. ReadOnly fields are omitted, as are immutable create fields.
type Model struct {
	Fields    []Field
	Values    map[string]string
	Errors    map[string]string
	Immutable []string
	Detail    string
	Create    bool
}

// Options supplies presentation and command addresses. Namespace is a DOM scope,
// separate from an example's source identity. Use a unique stable namespace per
// rendered instance. It must start with an ASCII letter and contain only letters,
// digits, underscores or hyphens. The resulting form ID is Namespace + "-form".
type Options struct {
	Namespace                              string
	Action, CancelURL, Title               string
	CancelLabel, SubmitLabel, FailureTitle string
}

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
// fixed scope. namespace_test.go feeds it the inputs that would break that and
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
	body := []g.Node{}
	if model.Detail != "" {
		body = append(body, examples.ExampleWithSlots(examples.ExampleInfo{ID: "error", ComponentID: "pk-ui.component.alert"},
			components.AlertProps{Tone: "danger", Title: cmp.Or(options.FailureTitle, "That could not be saved"), Message: model.Detail, Bordered: true},
			components.AlertSlots{}, components.AlertWithSlots).Node)
	}
	for _, field := range model.Fields {
		f := field.Definition
		immutable := slices.Contains(model.Immutable, f.Name)
		if f.ReadOnly || (model.Create && immutable) {
			continue
		}
		value, present := model.Values[f.Name]
		if !present && model.Create {
			value = f.Default
		}
		body = append(body, Control(ControlProps{Field: field,
			// Hex preserves arbitrary JSON names without collisions with control
			// error/help suffixes, while reordering leaves field identity intact.
			ID:    options.Namespace + "-field-" + hex.EncodeToString([]byte(f.Name)),
			Value: value, Error: model.Errors[f.Name], Immutable: immutable}))
	}
	body = append(body, examples.ExampleWithChildren(
		examples.ExampleInfo{ID: "actions", ComponentID: "pk-ui.component.formactions"}, components.FormActionsProps{}, []g.Node{
			examples.ExampleWithSlots(examples.ExampleInfo{ID: "cancel", ComponentID: "pk-ui.component.button"},
				components.ButtonProps{Label: cmp.Or(options.CancelLabel, "Cancel"), Variant: "secondary", Href: options.CancelURL}, components.ButtonSlots{}, components.ButtonWithSlots).Node,
			examples.ExampleWithSlots(examples.ExampleInfo{ID: "save", ComponentID: "pk-ui.component.button"},
				components.ButtonProps{Label: cmp.Or(options.SubmitLabel, "Save"), Type: "submit"}, components.ButtonSlots{}, components.ButtonWithSlots).Node,
		}, components.FormActions).Node)
	// A file control submits a body a urlencoded form cannot carry, and the
	// encoding belongs to the form rather than to whoever composed it: one
	// field that needs it is enough to break the whole submission. It is added
	// here and not to FormProps because nothing else may decide it — a page
	// that sets enctype by hand and renders no file control is a page that
	// stopped validating its own fields.
	props := components.FormProps{
		ComponentProps: components.ComponentProps{ID: formID},
		HTMXProps: components.HTMXProps{
			Post: options.Action, Target: "#" + formID, Swap: "outerHTML", Select: "#" + formID},
		Action: options.Action, Label: options.Title,
	}
	if rendersFile(model) {
		props.Attrs = map[string]string{"enctype": "multipart/form-data"}
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
		if f.ReadOnly || (model.Create && slices.Contains(model.Immutable, f.Name)) {
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

// hint joins the field's documentation with notes about its chosen control.
func hint(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, " ")
}

func immutableNote(immutable bool) string {
	if immutable {
		return "Changed by a command of its own, not by this form."
	}
	return ""
}
