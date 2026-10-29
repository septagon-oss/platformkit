package components

// input.go renders the text input and the field around it — label, hint, error
// and the aria-describedby that ties them together — and canonicalizes the type.

import (
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/ui/style"
)

// Input renders InputProps as a labelled form field. When Error is set
// the input carries aria-invalid and is described by the error element.
func Input(p InputProps) g.Node {
	return inputWithSlots(p, nil, nil)
}

func inputWithSlots(
	p InputProps,
	iconStart []g.Node,
	iconEnd []g.Node,
) g.Node {
	return inputFieldWithSlots("input", p, iconStart, iconEnd)
}

func inputFieldWithSlots(
	componentName string,
	p InputProps,
	iconStart []g.Node,
	iconEnd []g.Node,
) g.Node {
	if componentName == "" {
		componentName = "input"
	}
	id := p.ID
	if id == "" && p.Name != "" {
		id = "pk-" + componentName + "-" + p.Name
	}
	typ, validType := canonicalInputType(p.Type)
	if !validType {
		panic("pk-ui: unsupported Input type " + p.Type)
	}
	cl, tone, size, invalid := inputAppearance(p, iconStart, iconEnd)

	input := []g.Node{
		classes(cl.Compile(), p.Class),
		h.ID(id), h.Name(p.Name), h.Type(typ),
		g.Attr("data-tone", tone),
		g.Attr("data-size", size),
	}
	input = append(input, attrPairs(p.Attrs)...)
	input = append(input, htmxAttrs(p.HTMXProps)...)
	input = inputValueAttrs(p, typ, input)
	input = inputConstraintAttrs(p, input)
	input = inputDescriptionAttrs(p, id, invalid, input)

	fieldClass := clFieldWrap
	if p.FullWidth {
		fieldClass = clFieldWrapFull
	}
	field := []g.Node{h.Class(fieldClass.Compile()), g.Attr("data-component", componentName)}
	if typ == "hidden" {
		field = append(field, h.Hidden(""))
	}
	if p.Label != "" {
		field = append(field, labelWithText(LabelProps{For: id, Required: p.Required}, g.Group{
			g.Raw("<!--pk-text:label-->"), g.Text(p.Label), g.Raw("<!--/pk-text:label-->"),
		}))
	}
	field = append(field, inputIconControl(input, iconStart, iconEnd))
	if p.Error != "" {
		field = append(field, h.P(
			h.ID(id+"-error"),
			h.Class(clFieldErr.Compile()),
			h.Role("alert"),
			g.Text(p.Error),
		))
	}
	if hasHelp(p.HelpText, p.HelpNotes) {
		field = append(field, helpParagraph(p.HelpText, p.HelpNotes,
			h.ID(id+"-help"), h.Class(clHelp.Compile())))
	}
	return h.Div(field...)
}

func canonicalInputType(raw string) (string, bool) {
	typ := strings.ToLower(strings.TrimSpace(raw))
	if typ == "" {
		return "text", true
	}
	switch typ {
	case "text", "email", "password", "number", "tel", "url", "search",
		"date", "time", "datetime-local", "month", "week", "color", "hidden", "file":
		return typ, true
	default:
		return typ, false
	}
}

func inputAppearance(p InputProps, iconStart, iconEnd []g.Node) (style.ClassList, string, string, bool) {
	size := strings.ToLower(strings.TrimSpace(p.Size))
	if _, exists := clInputSize[size]; !exists {
		size = "md"
	}
	tone := strings.ToLower(strings.TrimSpace(p.Tone))
	if _, exists := clInputTone[tone]; !exists {
		tone = "neutral"
	}
	cl := clInput.
		Merge(clInputTone[tone]).
		Merge(clInputSize[size])
	invalid := p.Invalid || p.Error != ""
	if invalid {
		cl = clInput.
			Merge(clInputError).
			Merge(clInputSize[size])
	}
	if p.ReadOnly && !p.Disabled {
		cl = cl.Merge(clInputReadOnly)
	}
	if len(iconStart) > 0 {
		cl = cl.Merge(clInputPadStart)
	}
	if len(iconEnd) > 0 {
		cl = cl.Merge(clInputPadEnd)
	}
	return cl, tone, size, invalid
}

func inputValueAttrs(p InputProps, typ string, input []g.Node) []g.Node {
	if typ == "text" {
		input = append(input, g.Attr("data-pk-value", "value"))
	}
	// A file input takes no value: no browser lets a page choose a file for
	// somebody, and one that carried a value attribute would be a control the
	// form submits nothing for.
	if p.Value != "" && typ != "file" {
		input = append(input, h.Value(p.Value))
	}
	if typ == "file" {
		if p.Accept != "" {
			input = append(input, g.Attr("accept", p.Accept))
		}
		if p.Multiple {
			input = append(input, g.Attr("multiple", "multiple"))
		}
	}
	return input
}

func inputConstraintAttrs(p InputProps, input []g.Node) []g.Node {
	if p.Placeholder != "" {
		input = append(input, h.Placeholder(p.Placeholder))
	}
	if p.Required {
		input = append(input, h.Required())
	}
	if p.ReadOnly {
		input = append(input, h.ReadOnly())
	}
	if p.AutoFocus {
		input = append(input, h.AutoFocus())
	}
	if p.Disabled {
		input = append(input, h.Disabled())
	}
	if p.Min != "" {
		input = append(input, h.Min(p.Min))
	}
	if p.Max != "" {
		input = append(input, h.Max(p.Max))
	}
	if p.Step != "" {
		input = append(input, h.Step(p.Step))
	}
	if p.MinLength > 0 {
		input = append(input, g.Attr("minlength", itoa(p.MinLength)))
	}
	if p.MaxLength > 0 {
		input = append(input, h.MaxLength(itoa(p.MaxLength)))
	}
	if p.Pattern != "" {
		input = append(input, h.Pattern(p.Pattern))
	}
	if p.Autocomplete != "" {
		input = append(input, h.AutoComplete(p.Autocomplete))
	}
	return input
}

func inputDescriptionAttrs(p InputProps, id string, invalid bool, input []g.Node) []g.Node {
	describedBy := make([]string, 0, 2)
	if invalid {
		input = append(input, g.Attr("aria-invalid", "true"))
	}
	if p.Error != "" {
		describedBy = append(describedBy, id+"-error")
	}
	if hasHelp(p.HelpText, p.HelpNotes) {
		describedBy = append(describedBy, id+"-help")
	}
	if len(describedBy) > 0 {
		input = append(input, g.Attr("aria-describedby", strings.Join(describedBy, " ")))
	}
	if p.Label == "" && p.Name != "" {
		input = append(input, g.Attr("aria-label", p.Name))
	}
	return input
}

func inputIconControl(input, iconStart, iconEnd []g.Node) g.Node {
	control := h.Input(input...)
	if len(iconStart) > 0 || len(iconEnd) > 0 {
		controlChildren := []g.Node{h.Class(clInputIconWrap.Compile())}
		if len(iconStart) > 0 {
			controlChildren = append(controlChildren, h.Span(
				h.Class(clInputIconStart.Compile()),
				g.Group(iconStart),
			))
		}
		controlChildren = append(controlChildren, control)
		if len(iconEnd) > 0 {
			controlChildren = append(controlChildren, h.Span(
				h.Class(clInputIconEnd.Compile()),
				g.Group(iconEnd),
			))
		}
		return h.Div(controlChildren...)
	} else {
		return control
	}
}
