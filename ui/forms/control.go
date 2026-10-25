package forms

import (
	"cmp"
	"slices"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	g "maragu.dev/gomponents"
)

// ControlProps describes one field's presentation. ID is the trusted DOM identity;
// leaving it empty uses the component's historical name-derived default. Enclosing
// forms should use Example, which supplies a distinct identity to every control.
type ControlProps struct {
	Field            Field
	ID, Value, Error string
	Immutable        bool
	// Format answers this control's chrome in the request's language. It is the
	// kernel's own formatter, so the provider keeps ownership of interpolation,
	// plural rules and regional number format. Nil — the default — answers every
	// phrase with its source text.
	Format locale.Formatter
	// Text answers one phrase that carries no value. Format answers every phrase,
	// and Format is asked first whenever both are composed.
	//
	// Deprecated: a phrase that asks for a value is answered from its source text
	// and never asked of Text, so this seam answers only the phrases carrying none.
	// Compose Format instead.
	Text func(key, fallback string) string
}

// phrases is this control's chrome, asked of the formatter the render composed.
func (p ControlProps) phrases() phraser { return phrasesFor(p.Format, p.Text) }

// widgetInputType names the <input> type each widget that draws an input asks
// for. The rest of entity.Widgets draw a component (select, textarea), a
// decorated input (entity-picker) or the bool control (checkbox), so they have
// no input type to name. a test renders one control per name
// in that vocabulary and refuses a name with no drawn control, which is what
// keeps this table and that list from drifting apart again: `file` was added to
// the component earlier and reached no schema until now.
var widgetInputType = map[string]string{
	"color":    "color",
	"date":     "date",
	"datetime": "datetime-local",
	"email":    "email",
	"file":     "file",
	"hidden":   "hidden",
	"month":    "month",
	"number":   "number",
	"password": "password",
	"search":   "search",
	"tel":      "tel",
	"text":     "text",
	"time":     "time",
	"url":      "url",
	"week":     "week",
}

// Control renders the existing component chosen by the entity widget/type.
// Its capture retains "field/" plus the schema name and Core's Props contract.
func Control(p ControlProps) g.Node {
	f := p.Field.Definition
	word := p.phrases()
	label, name := cmp.Or(p.Field.Label, f.Name), f.Name
	base := components.InputProps{
		ComponentProps: components.ComponentProps{ID: p.ID},
		Name:           name, Label: label, Value: p.Value, Error: p.Error,
		HelpText: cmp.Or(p.Field.Help, f.Doc), HelpNotes: controlNotes(p, word),
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
			// The label arrives as its author wrote it and the message owns the
			// sentence around it: casing a translated label is a rule one language has
			// and the next language cannot read back out of the result.
			placeholder = word("forms.field.choose", "Choose a %[1]s", label)
		}
		return examples.ExampleOf(examples.ExampleInfo{ID: "field/" + name, ComponentID: "pk-ui.component.select", Name: label}, components.SelectProps{
			ComponentProps: components.ComponentProps{ID: p.ID, Disabled: p.Immutable},
			Name:           name, Label: label, Value: p.Value, Error: p.Error,
			Required: f.Required, Options: options, Placeholder: placeholder,
			HelpText: base.HelpText, HelpNotes: base.HelpNotes, FullWidth: true,
		}, components.Select).Node
	case f.Widget == "textarea":
		return examples.ExampleOf(examples.ExampleInfo{ID: "field/" + name, ComponentID: "pk-ui.component.textarea", Name: label}, components.TextareaProps{
			ComponentProps: components.ComponentProps{ID: p.ID, Disabled: p.Immutable},
			Name:           name, Label: label, Value: p.Value, ErrorMessage: p.Error,
			Required: f.Required, Rows: 5, FullWidth: true, HelperText: base.HelpText,
			HelperNotes: base.HelpNotes,
		}, components.Textarea).Node
	case f.Widget == "richtext":
		return richtextField(p, base, word)
	case f.Widget == "checkbox" || f.Type == entity.TypeBool:
		return examples.ExampleOf(examples.ExampleInfo{ID: "field/" + name, ComponentID: "pk-ui.component.checkbox", Name: label}, components.CheckboxProps{
			ComponentProps: components.ComponentProps{ID: p.ID, Disabled: p.Immutable},
			Name:           name, Label: label, Value: "true", Checked: p.Value == "true",
			Required: f.Required, Error: p.Error, HelpText: base.HelpText, HelpNotes: base.HelpNotes,
		}, components.Checkbox).Node
	}
	// The input type is what the widget named, and only then what the Go type
	// implies. A widget that names one wins over the type because that is what
	// the tag is for: `datetime` on a string holding an instant and the same
	// widget on its time.Time neighbour now draw the same control, where the
	// name used to be discarded and the string became a plain text box.
	switch named, byType := widgetInputType[f.Widget]; {
	case byType:
		base.Type = named
	case f.Type == entity.TypeTime:
		base.Type = "datetime-local"
	case f.Type == entity.TypeInt:
		base.Type, base.Step = "number", "1"
	case f.Type == entity.TypeFloat:
		base.Type, base.Step = "number", "any"
	}
	// datetime-local carries no seconds and no zone, so a stored instant is cut
	// to the minute the control can actually show. The value is not reformatted:
	// what a form submits is what the field's own validation will read.
	if base.Type == "datetime-local" && len(base.Value) >= 16 {
		base.Value = base.Value[:16]
	}
	return examples.ExampleOf(examples.ExampleInfo{ID: "field/" + name, ComponentID: "pk-ui.component.input", Name: label}, base, components.Input).Node
}

// controlNotes are the lines this package has to say about the control it chose.
// A note says something about that control, not about the field, so it stands as
// its own sentence beside the field's own line instead of being joined onto it:
// two owners' sentences, one paragraph, neither assembled out of the other.
func controlNotes(p ControlProps, word phraser) []string {
	f := p.Field.Definition
	var notes []string
	switch {
	case f.Widget == "entity-picker":
		notes = append(notes, word("forms.field.note.entityPicker",
			"The identifier of the related record. There is no picker for it yet."))
	case f.Type == entity.TypeList:
		notes = append(notes, word("forms.field.note.list", "Comma separated."))
	}
	if p.Immutable {
		notes = append(notes, word("forms.field.note.immutable",
			"Changed by a command of its own, not by this form."))
	}
	return notes
}
