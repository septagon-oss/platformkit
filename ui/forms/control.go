package forms

import (
	"cmp"
	"slices"
	"strings"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/components"
	g "maragu.dev/gomponents"
)

// ControlProps describes one field's presentation. ID is the trusted DOM identity;
// leaving it empty uses the component's historical name-derived default. Enclosing
// forms should use Example, which supplies a distinct identity to every control.
type ControlProps struct {
	Field            Field
	ID, Value, Error string
	Immutable        bool
}

// Control renders the existing component chosen by the entity widget/type.
// Its capture retains "field/" plus the schema name and Core's Props contract.
func Control(p ControlProps) g.Node {
	f := p.Field.Definition
	label, name := cmp.Or(p.Field.Label, f.Name), f.Name
	base := components.InputProps{
		ComponentProps: components.ComponentProps{ID: p.ID},
		Name:           name, Label: label, Value: p.Value, Error: p.Error,
		HelpText: hint(f.Doc, immutableNote(p.Immutable)),
		Required: f.Required, ReadOnly: p.Immutable, FullWidth: true,
	}
	switch {
	case len(f.Enum) > 0 || f.Widget == "select":
		options := slices.Clone(p.Field.Options)
		if options == nil {
			options = make([]components.SelectOption, 0, len(f.Enum))
			for _, value := range f.Enum {
				options = append(options, components.SelectOption{Label: value, Value: value})
			}
		}
		placeholder := ""
		if f.Default == "" {
			placeholder = "Choose a " + strings.ToLower(label)
		}
		return components.ExampleOf(components.ExampleInfo{ID: "field/" + name, ComponentID: "pk-ui.component.select", Name: label}, components.SelectProps{
			ComponentProps: components.ComponentProps{ID: p.ID, Disabled: p.Immutable},
			Name:           name, Label: label, Value: p.Value, Error: p.Error,
			Required: f.Required, Options: options, Placeholder: placeholder,
			HelpText: base.HelpText,
		}, components.Select).Node
	case f.Widget == "textarea":
		return components.ExampleOf(components.ExampleInfo{ID: "field/" + name, ComponentID: "pk-ui.component.textarea", Name: label}, components.TextareaProps{
			ComponentProps: components.ComponentProps{ID: p.ID, Disabled: p.Immutable},
			Name:           name, Label: label, Value: p.Value, ErrorMessage: p.Error,
			Required: f.Required, Rows: 5, FullWidth: true, HelperText: base.HelpText,
		}, components.Textarea).Node
	case f.Type == entity.TypeBool:
		return components.ExampleOf(components.ExampleInfo{ID: "field/" + name, ComponentID: "pk-ui.component.checkbox", Name: label}, components.CheckboxProps{
			ComponentProps: components.ComponentProps{ID: p.ID, Disabled: p.Immutable},
			Name:           name, Label: label, Value: "true", Checked: p.Value == "true",
			Required: f.Required, Error: p.Error, HelpText: base.HelpText,
		}, components.Checkbox).Node
	case f.Widget == "entity-picker":
		base.HelpText = hint(base.HelpText, "The identifier of the related record. There is no picker for it yet.")
	case f.Type == entity.TypeList:
		base.HelpText = hint(base.HelpText, "Comma separated.")
	case f.Type == entity.TypeTime:
		base.Type = "datetime-local"
		if len(base.Value) >= 16 {
			base.Value = base.Value[:16]
		}
	case f.Type == entity.TypeInt:
		base.Type, base.Step = "number", "1"
	case f.Type == entity.TypeFloat:
		base.Type, base.Step = "number", "any"
	}
	return components.ExampleOf(components.ExampleInfo{ID: "field/" + name, ComponentID: "pk-ui.component.input", Name: label}, base, components.Input).Node
}
