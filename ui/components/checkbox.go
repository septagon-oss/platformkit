package components

// checkbox.go renders the checkbox, its label and the error it announces.

import (
	"maps"
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// Checkbox renders CheckboxProps as a labelled native checkbox with a
// token-owned indicator. The native control remains in the accessibility tree;
// the shared checkbox controller only synchronizes indeterminate state and the
// visual projection because HTML has no declarative indeterminate attribute.
func Checkbox(p CheckboxProps) g.Node {
	id := p.ID
	if id == "" && p.Name != "" {
		id = "pk-checkbox-" + p.Name
	}
	state := "unchecked"
	if p.Checked {
		state = "checked"
	}
	if p.Indeterminate {
		state = "indeterminate"
	}
	indeterminate := "false"
	if p.Indeterminate {
		indeterminate = "true"
	}

	rootClass := clCheckboxRoot
	if p.Disabled {
		rootClass = rootClass.Merge(clCheckboxRootDisabled)
	}
	root := []g.Node{
		classes(rootClass.Compile(), p.Class),
		g.Attr("data-component", "checkbox"),
		g.Attr("data-controller", "checkbox"),
		g.Attr("data-checkbox-indeterminate-value", indeterminate),
		g.Attr("data-checkbox-active-classes", clCheckboxIndicatorActive.Compile()),
		g.Attr("data-checkbox-inactive-classes", clCheckboxIndicatorIdle.Compile()),
		g.Attr("data-state", state),
	}
	if id != "" {
		root = append(root, h.For(id))
	}
	if p.Hidden {
		root = append(root, g.Attr("hidden"))
	}

	// The visible label owns the anchor; the native input is visually hidden.
	if anchor := p.Attrs["data-screen-anchor"]; anchor != "" {
		root = append(root, g.Attr("data-screen-anchor", anchor))
	}

	box := checkboxInputAttrs(p, id)
	root = append(root, h.Input(box...), checkboxIndicator(p, state))
	if p.Label != "" {
		root = append(root, h.Span(h.Class(clCheckboxLabel.Compile()), g.Text(p.Label)))
	}
	control := h.Label(root...)
	if !hasHelp(p.HelpText, p.HelpNotes) && p.Error == "" {
		return control
	}
	field := []g.Node{h.Class(clFieldWrap.Compile()), control}
	if p.Error != "" {
		field = append(field, h.P(h.ID(id+"-error"), h.Class(clFieldErr.Compile()), h.Role("alert"), g.Text(p.Error)))
	}
	if hasHelp(p.HelpText, p.HelpNotes) {
		field = append(field, helpParagraph(p.HelpText, p.HelpNotes,
			h.ID(id+"-help"), h.Class(clHelp.Compile())))
	}
	return h.Div(field...)
}

func checkboxInputAttrs(p CheckboxProps, id string) []g.Node {
	box := []g.Node{
		h.Class(clCheckboxInput.Compile()), h.Type("checkbox"),
		g.Attr("data-checkbox-input", "true"),
	}
	if id != "" {
		box = append(box, h.ID(id))
	}
	if p.Name != "" {
		box = append(box, h.Name(p.Name))
	}
	attrs := maps.Clone(p.Attrs)
	delete(attrs, "data-screen-anchor")
	box = append(box, attrPairs(attrs)...)
	if p.Value != "" {
		box = append(box, h.Value(p.Value))
	}
	if p.Checked {
		box = append(box, h.Checked())
	}
	if p.Required {
		box = append(box, h.Required())
	}
	if p.Disabled {
		box = append(box, h.Disabled())
	}
	// Native indeterminate state is a DOM property, set by the controller;
	// aria-checked would disagree with the native input before it runs.
	if p.Label == "" && p.Name != "" {
		box = append(box, g.Attr("aria-label", p.Name))
	}
	describedBy := make([]string, 0, 2)
	if p.Error != "" {
		box = append(box, g.Attr("aria-invalid", "true"))
		describedBy = append(describedBy, id+"-error")
	}
	if hasHelp(p.HelpText, p.HelpNotes) {
		describedBy = append(describedBy, id+"-help")
	}
	if len(describedBy) > 0 {
		box = append(box, g.Attr("aria-describedby", strings.Join(describedBy, " ")))
	}

	return box
}

func checkboxIndicator(p CheckboxProps, state string) g.Node {
	indicatorClass := clCheckboxIndicator.Merge(clCheckboxIndicatorIdle)
	if p.Checked || p.Indeterminate {
		indicatorClass = clCheckboxIndicator.Merge(clCheckboxIndicatorActive)
	}
	checkmark := []g.Node{
		h.Class(clCheckboxCheckmark.Compile()),
		g.Attr("viewBox", "0 0 20 20"),
		g.Attr("fill", "currentColor"),
		g.Attr("data-checkbox-checkmark", "true"),
		g.El("path",
			g.Attr("fill-rule", "evenodd"),
			g.Attr("clip-rule", "evenodd"),
			g.Attr("d", "M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z"),
		),
	}
	if !p.Checked || p.Indeterminate {
		checkmark = append(checkmark, g.Attr("hidden"))
	}
	bar := []g.Node{
		h.Class(clCheckboxBar.Compile()),
		g.Attr("data-checkbox-bar", "true"),
	}
	if !p.Indeterminate {
		bar = append(bar, g.Attr("hidden"))
	}
	return h.Span(
		h.Class(indicatorClass.Compile()),
		g.Attr("data-checkbox-box", "true"),
		g.Attr("data-state", state),
		g.Attr("aria-hidden", "true"),
		g.El("svg", checkmark...),
		h.Span(bar...),
	)
}
