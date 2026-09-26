package components

// checkbox.go renders the checkbox, its label and the error it announces.

import (
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
	box = append(box, attrPairs(p.Attrs)...)
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
	// No aria-checked here. This is a real checkbox, and ARIA's mixed state is
	// for an element that says role="checkbox" and owns its own state: on a
	// native input the property is `indeterminate`, which is a DOM property and
	// not an attribute, so it cannot be server-rendered at all. The attribute
	// below is what a controller sets it from, and the bar is what a person
	// sees; announcing "mixed" while the input reports unchecked was the two
	// disagreeing, which is what axe's aria-conditional-attr is about.
	if p.Label == "" && p.Name != "" {
		box = append(box, g.Attr("aria-label", p.Name))
	}
	describedBy := make([]string, 0, 2)
	if p.Error != "" {
		box = append(box, g.Attr("aria-invalid", "true"))
		describedBy = append(describedBy, id+"-error")
	}
	if p.HelpText != "" {
		describedBy = append(describedBy, id+"-help")
	}
	if len(describedBy) > 0 {
		box = append(box, g.Attr("aria-describedby", strings.Join(describedBy, " ")))
	}

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
	root = append(root,
		h.Input(box...),
		h.Span(
			h.Class(indicatorClass.Compile()),
			g.Attr("data-checkbox-box", "true"),
			g.Attr("data-state", state),
			g.Attr("aria-hidden", "true"),
			g.El("svg", checkmark...),
			h.Span(bar...),
		),
	)
	if p.Label != "" {
		root = append(root, h.Span(h.Class(clCheckboxLabel.Compile()), g.Text(p.Label)))
	}
	control := h.Label(root...)
	if p.HelpText == "" && p.Error == "" {
		return control
	}
	field := []g.Node{h.Class(clFieldWrap.Compile()), control}
	if p.Error != "" {
		field = append(field, h.P(h.ID(id+"-error"), h.Class(clFieldErr.Compile()), h.Role("alert"), g.Text(p.Error)))
	}
	if p.HelpText != "" {
		field = append(field, h.P(h.ID(id+"-help"), h.Class(clHelp.Compile()), g.Text(p.HelpText)))
	}
	return h.Div(field...)
}
