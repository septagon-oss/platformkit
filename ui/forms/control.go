package forms

import (
	"cmp"
	"slices"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	g "maragu.dev/gomponents"
)

// ControlProps describes one field's presentation. ID is the trusted DOM identity;
// leaving it empty uses the component's historical name-derived default. Enclosing
// forms should use Example, which supplies a distinct identity to every control.
type ControlProps struct {
	ChoiceValues     []string
	Field            Field
	ID, Value, Error string
	Immutable        bool
	AutoFocus        bool
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
	f.Widget = present.ControlKind(f)
	attrs := anchorAttrs(p.Field.Anchor)
	if f.Widget == "hidden" || f.Widget == "richtext" {
		attrs = nil
	}
	if p.AutoFocus {
		if attrs == nil {
			attrs = map[string]string{}
		}
		// A stepped form sends a person back to the step that refused them, and the
		// control that refused is the one that should hold the caret.
		attrs["autofocus"] = "autofocus"
	}
	if f.Choices != nil || entity.ChoiceAppearance(f.Widget) {
		return choiceControl(p, p.Field.Anchor)
	}
	word := p.phrases()
	label, name := cmp.Or(p.Field.Label, f.Name), f.Name
	base := components.InputProps{
		ComponentProps: components.ComponentProps{ID: p.ID, Attrs: attrs},
		Name:           name, Label: label, Value: p.Value, Error: p.Error,
		HelpText: cmp.Or(p.Field.Help, f.Doc), HelpNotes: controlNotes(p, word),
		Required: f.Required, ReadOnly: p.Immutable, FullWidth: true,
	}
	switch {
	case f.Widget == "select" || f.Widget == "" && len(f.Enum) > 0:
		return selectControl(p, base, word)
	case f.Widget == "textarea":
		return examples.ExampleOf(examples.ExampleInfo{ID: "field/" + name, ComponentID: "pk-ui.component.textarea", Name: label}, components.TextareaProps{
			ComponentProps: components.ComponentProps{ID: p.ID, Disabled: p.Immutable, Attrs: attrs},
			Name:           name, Label: label, Value: p.Value, ErrorMessage: p.Error,
			Required: f.Required, Rows: 5, FullWidth: true, HelperText: base.HelpText,
			HelperNotes: base.HelpNotes,
		}, components.Textarea).Node
	case f.Widget == "richtext":
		return richtextControl(p, label, base, word)
	case f.Widget == "checkbox" || f.Widget == "" && f.Type == entity.TypeBool:
		return examples.ExampleOf(examples.ExampleInfo{ID: "field/" + name, ComponentID: "pk-ui.component.checkbox", Name: label}, components.CheckboxProps{
			ComponentProps: components.ComponentProps{ID: p.ID, Disabled: p.Immutable, Attrs: attrs},
			Name:           name, Label: label, Value: "true", Checked: p.Value == "true",
			Required: f.Required, Error: p.Error, HelpText: base.HelpText, HelpNotes: base.HelpNotes,
		}, components.Checkbox).Node
	}
	base.Type, base.Step = inputType(f)
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

func inputType(f entity.Field) (string, string) {
	kind, step := widgetInputType[f.Widget], ""
	if kind == "" {
		switch f.Type {
		case entity.TypeTime:
			kind = "datetime-local"
		case entity.TypeInt, entity.TypeFloat:
			kind = "number"
		default:
			kind = "text"
		}
	}
	if kind == "number" {
		if f.Type == entity.TypeFloat {
			step = "any"
		} else if f.Type == entity.TypeInt {
			step = "1"
		}
	}
	return kind, step
}

// selectControl draws the one select this field's widget asks for. The label
// arrives as its author wrote it and the message owns the sentence around it:
// casing a translated label is a rule one language has and the next language
// cannot read back out of the result.
func selectControl(p ControlProps, base components.InputProps, word phraser) g.Node {
	f := p.Field.Definition
	name, label := base.Name, base.Label
	options := slices.Clone(p.Field.Options)
	if options == nil {
		options = make([]components.SelectOption, 0, len(f.Enum))
		for _, value := range f.Enum {
			options = append(options, components.SelectOption{Label: value, Value: value})
		}
	}
	placeholder := ""
	if f.Default == "" {
		placeholder = word("forms.field.choose", "Choose a %[1]s", label)
	}
	return examples.ExampleOf(examples.ExampleInfo{ID: "field/" + name, ComponentID: "pk-ui.component.select", Name: label}, components.SelectProps{
		ComponentProps: components.ComponentProps{ID: p.ID, Disabled: p.Immutable, Attrs: base.Attrs},
		Name:           name, Label: label, Value: p.Value, Error: p.Error,
		Required: f.Required, Options: options, Placeholder: placeholder,
		HelpText: base.HelpText, HelpNotes: base.HelpNotes, FullWidth: true,
	}, components.Select).Node
}
