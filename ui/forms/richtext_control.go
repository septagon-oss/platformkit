package forms

import (
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

func richtextControl(p ControlProps, label string, base components.InputProps, word phraser) g.Node {
	f := p.Field.Definition
	name := f.Name
	// The textarea is the field a stepped form focuses when the step refused, so
	// the attributes the caller gathered reach it as they reach any other control.
	attrs := base.Attrs
	area := examples.ExampleOf(examples.ExampleInfo{ID: "field/" + name, ComponentID: "pk-ui.component.textarea", Name: label}, components.TextareaProps{
		ComponentProps: components.ComponentProps{ID: p.ID, Disabled: p.Immutable, Attrs: attrs},
		Name:           name, Label: label, Value: p.Value, ErrorMessage: p.Error,
		Required: f.Required, Rows: 12, FullWidth: true, HelperText: base.HelpText,
		HelperNotes: base.HelpNotes, MaxLength: f.MaxLength, CharacterLimitOnly: true,
	}, components.Textarea).Node
	// Replace this native disclosure when the shared disclosure lands.
	help := h.Details(
		h.Summary(g.Text(word("forms.richtext.help", "Formatting help"))),
		h.Ul(
			h.Li(g.Text(word("forms.richtext.paragraphs", "Paragraphs: leave a blank line; a hard break ends with \\."))),
			h.Li(g.Text(word("forms.richtext.headings", "Headings: ## through ####."))),
			h.Li(g.Text(word("forms.richtext.marks", "Bold **text**, italic *text*, strike ~~text~~, inline `code`."))),
			h.Li(g.Text(word("forms.richtext.links", "Links: [label](https://example.com) or [label](/page)."))),
			h.Li(g.Text(word("forms.richtext.lists", "Lists: - item, 1. item or - [ ] task; up to three levels."))),
			h.Li(g.Text(word("forms.richtext.blocks", "Quote: > text; code: ```language; rule: ---."))),
			h.Li(g.Text(word("forms.richtext.tables", "Tables: | heading | with a | --- | separator."))),
			h.Li(g.Text(word("forms.richtext.images", "Images: ![alt](pk-file:uuid \"caption\") alone on a line. Upload the file first."))),
		),
	)
	// The editor's chrome is drawn beside the textarea rather than instead of
	// it: with no JavaScript the field is the plain textarea above with the
	// guidance below it, and the submitted value is the same either way.
	return richtextChrome(p, area, help)
}
