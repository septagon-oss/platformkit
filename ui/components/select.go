package components

// select.go renders the select, grouping its options exactly as the caller
// ordered them and marking the selected ones.

import (
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// Select renders SelectProps as a labelled native <select>, styled as
// an input-family control. A Placeholder renders as an empty leading option
// so an untouched control has no accidental value; when the field is
// Required the placeholder is how "nothing chosen yet" stays expressible.
func Select(p SelectProps) g.Node {
	id := p.ID
	if id == "" && p.Name != "" {
		id = "pk-select-" + p.Name
	}
	single := !p.Multiple && p.VisibleRows <= 1
	size := clInputSize["md"]
	if single {
		size = clSelectSize
	}
	cl := clInput.Merge(clInputNormal).Merge(size)
	if p.Error != "" {
		cl = clInput.Merge(clInputError).Merge(size)
	}

	selectedValues := make(map[string]struct{}, len(p.Values)+1)
	for _, value := range p.Values {
		selectedValues[value] = struct{}{}
	}
	if p.Value != "" {
		selectedValues[p.Value] = struct{}{}
	}

	var options []g.Node
	if !p.Multiple && (p.Placeholder != "" || !p.Required) {
		label := strings.TrimSpace(p.Placeholder)
		if label == "" {
			label = "Choose…"
		}
		placeholder := []g.Node{h.Value(""), g.Text(label)}
		if p.Placeholder != "" || p.Required {
			placeholder = append(placeholder, h.Disabled())
		}
		if len(selectedValues) == 0 {
			placeholder = append(placeholder, h.Selected())
		}
		options = append(options, h.Option(placeholder...))
	}
	for _, group := range groupSelectOptions(p.Options) {
		groupOptions := make([]g.Node, 0, len(group.Options))
		for _, option := range group.Options {
			groupOptions = append(groupOptions, renderSelectOption(option, selectedValues))
		}
		if group.Name == "" {
			options = append(options, groupOptions...)
			continue
		}
		options = append(options, g.El(
			"optgroup",
			g.Attr("label", group.Name),
			g.Group(groupOptions),
		))
	}

	sel := []g.Node{
		classes(cl.Compile(), p.Class),
		h.ID(id), h.Name(p.Name),
		g.Attr("data-pk-value", "value"),
		g.Attr("data-pk-values", "values"),
		g.Attr("data-pk-options", "options"),
	}
	sel = append(sel, attrPairs(p.Attrs)...)
	sel = append(sel, htmxAttrs(p.HTMXProps)...)
	if p.Required {
		sel = append(sel, h.Required())
	}
	if p.Multiple {
		sel = append(sel, g.Attr("multiple", ""))
	}
	visibleRows := p.VisibleRows
	if p.Multiple && visibleRows <= 0 {
		visibleRows = 4
	}
	if visibleRows > 0 {
		sel = append(sel, g.Attr("size", itoa(visibleRows)))
	}
	if p.Disabled {
		sel = append(sel, h.Disabled())
	}
	describedBy := make([]string, 0, 2)
	if p.Error != "" {
		sel = append(sel, g.Attr("aria-invalid", "true"))
		describedBy = append(describedBy, id+"-error")
	}
	if p.HelpText != "" {
		describedBy = append(describedBy, id+"-help")
	}
	if len(describedBy) > 0 {
		sel = append(sel, g.Attr("aria-describedby", strings.Join(describedBy, " ")))
	}
	if p.Label == "" && p.Name != "" {
		sel = append(sel, g.Attr("aria-label", p.Name))
	}
	sel = append(sel, options...)

	fieldClass := clFieldWrap
	if p.FullWidth {
		fieldClass = clFieldWrapFull
	}
	field := []g.Node{
		h.Class(fieldClass.Compile()),
		g.Attr("data-component", "select"),
	}
	if p.Label != "" {
		field = append(field, labelWithText(LabelProps{For: id, Required: p.Required}, g.Group{
			g.Raw("<!--pk-text:label-->"), g.Text(p.Label), g.Raw("<!--/pk-text:label-->"),
		}))
	}
	control := h.Select(sel...)
	if single {
		control = h.Div(h.Class(clSelectGrid.Compile()), control,
			h.Div(h.Class(clSelectIndicator.Compile()), Icon(IconProps{Name: "chevron-down", Size: "sm"})))
	}
	field = append(field, control)
	if p.Error != "" {
		field = append(field, h.P(
			h.ID(id+"-error"),
			h.Class(clFieldErr.Compile()),
			h.Role("alert"),
			g.Text(p.Error),
		))
	}
	if p.HelpText != "" {
		field = append(field, h.P(h.ID(id+"-help"), h.Class(clHelp.Compile()), g.Text(p.HelpText)))
	}
	return h.Div(field...)
}

type selectOptionGroup struct {
	Name    string
	Options []SelectOption
}

func groupSelectOptions(options []SelectOption) []selectOptionGroup {
	groups := make([]selectOptionGroup, 0)
	indices := make(map[string]int)
	for _, option := range options {
		name := strings.TrimSpace(option.Group)
		index, exists := indices[name]
		if !exists {
			index = len(groups)
			indices[name] = index
			groups = append(groups, selectOptionGroup{Name: name})
		}
		groups[index].Options = append(groups[index].Options, option)
	}
	return groups
}

func renderSelectOption(option SelectOption, selectedValues map[string]struct{}) g.Node {
	label := strings.TrimSpace(option.Label)
	if label == "" {
		label = option.Value
	}
	children := []g.Node{h.Value(option.Value), g.Text(label)}
	if _, selected := selectedValues[option.Value]; selected {
		children = append(children, h.Selected())
	}
	if option.Disabled {
		children = append(children, h.Disabled())
	}
	if description := strings.TrimSpace(option.Description); description != "" {
		children = append(children, g.Attr("title", description))
	}
	return h.Option(children...)
}
