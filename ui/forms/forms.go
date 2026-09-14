// Package forms composes portable entity forms from detached presentation inputs.
// Handlers own authorization, parsing, persistence and the meaning of errors.
package forms

import (
	"cmp"
	"encoding/hex"
	"errors"
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
		if name == "" || seen[name] {
			return examples.Example{}, ErrFields
		}
		seen[name] = true
	}
	return example(id, model, options, false), nil
}

// LegacyExample retains the generated screens' historical DOM IDs while they
// migrate to explicit per-instance namespaces. It still uses the same builder.
//
// Deprecated: use Example for new compositions. Legacy forms cannot share a page
// when their field names overlap; their form ID is always "screen-form".
func LegacyExample(id string, model Model, options Options) examples.Example {
	return example(id, model, options, true)
}

func example(id string, model Model, options Options, legacy bool) examples.Example {
	formID := options.Namespace + "-form"
	if legacy {
		formID = "screen-form"
	}
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
		controlID := ""
		if !legacy {
			// Hex preserves arbitrary JSON names without collisions with control
			// error/help suffixes, while reordering leaves field identity intact.
			controlID = options.Namespace + "-field-" + hex.EncodeToString([]byte(f.Name))
		}
		body = append(body, Control(ControlProps{Field: field, ID: controlID,
			Value: value, Error: model.Errors[f.Name], Immutable: immutable}))
	}
	body = append(body, examples.ExampleWithChildren(
		examples.ExampleInfo{ID: "actions", ComponentID: "pk-ui.component.formactions"}, components.FormActionsProps{}, []g.Node{
			examples.ExampleWithSlots(examples.ExampleInfo{ID: "cancel", ComponentID: "pk-ui.component.button"},
				components.ButtonProps{Label: cmp.Or(options.CancelLabel, "Cancel"), Variant: "secondary", Href: options.CancelURL}, components.ButtonSlots{}, components.ButtonWithSlots).Node,
			examples.ExampleWithSlots(examples.ExampleInfo{ID: "save", ComponentID: "pk-ui.component.button"},
				components.ButtonProps{Label: cmp.Or(options.SubmitLabel, "Save"), Type: "submit"}, components.ButtonSlots{}, components.ButtonWithSlots).Node,
		}, components.FormActions).Node)
	return examples.ExampleWithChildren(
		examples.ExampleInfo{ID: id, ComponentID: "pk-ui.component.form", Group: "Screens", Name: options.Title}, components.FormProps{
			ComponentProps: components.ComponentProps{ID: formID},
			HTMXProps: components.HTMXProps{
				Post: options.Action, Target: "#" + formID, Swap: "outerHTML", Select: "#" + formID},
			Action: options.Action, Label: options.Title,
		}, body, components.Form)
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
