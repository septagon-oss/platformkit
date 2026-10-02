package components

// textarea.go renders the multi-line input and the same field chrome the single
// line one wears.

import (
	"strings"
	"unicode/utf8"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// Textarea renders TextareaProps as a labelled multi-line field.
func Textarea(p TextareaProps) g.Node {
	id := p.ID
	if id == "" && p.Name != "" {
		id = "pk-textarea-" + p.Name
	}
	cl := clInput.Merge(clInputNormal).Merge(variantOr(clInputSize, "md", "md"))
	if p.ErrorMessage != "" {
		cl = clInput.Merge(clInputError).Merge(variantOr(clInputSize, "md", "md"))
	}
	if p.AutoResize {
		cl = cl.Merge(clTextareaAuto)
	} else {
		cl = cl.Merge(clTextareaManual)
	}
	rows := p.Rows
	if rows <= 0 {
		rows = 4
	}
	minRows := p.MinRows
	if minRows <= 0 {
		minRows = 2
	}
	maxRows := p.MaxRows
	if maxRows < minRows {
		maxRows = max(minRows, 10)
	}
	if p.AutoResize {
		rows = minRows
	}
	showCount := p.ShowCount || p.MaxLength > 0
	describedBy := make([]string, 0, 2)
	if id != "" && p.ErrorMessage != "" {
		describedBy = append(describedBy, id+"-error")
	}
	if id != "" && p.HelperText != "" {
		describedBy = append(describedBy, id+"-helper")
	}
	area := []g.Node{
		classes(cl.Compile(), p.Class),
		h.Name(p.Name), h.Rows(itoa(rows)),
		g.Attr("data-textarea-input", ""),
		g.Attr("data-pk-value", "value"),
	}
	if id != "" {
		area = append(area, h.ID(id))
	}
	area = append(area, attrPairs(p.Attrs)...)
	area = append(area, htmxAttrs(p.HTMXProps)...)
	if p.Placeholder != "" {
		area = append(area, h.Placeholder(p.Placeholder))
	}
	if p.Required {
		area = append(area, h.Required())
	}
	if p.ReadOnly {
		area = append(area, h.ReadOnly())
	}
	if p.Disabled {
		area = append(area, h.Disabled())
	}
	if p.MinLength > 0 {
		area = append(area, g.Attr("minlength", itoa(p.MinLength)))
	}
	if p.MaxLength > 0 {
		area = append(area, h.MaxLength(itoa(p.MaxLength)))
	}
	if p.ErrorMessage != "" {
		area = append(area, g.Attr("aria-invalid", "true"))
	}
	if len(describedBy) > 0 {
		area = append(area, g.Attr("aria-describedby", strings.Join(describedBy, " ")))
	}
	if p.Label == "" && p.Name != "" {
		area = append(area, g.Attr("aria-label", p.Name))
	}
	actions := make([]string, 0, 2)
	if p.AutoResize {
		area = append(area,
			g.Attr("data-controller", "autoresize"),
			g.Attr("data-autoresize-min-rows-value", itoa(minRows)),
			g.Attr("data-autoresize-max-rows-value", itoa(maxRows)),
		)
		actions = append(actions, "input->autoresize#resize")
	}
	if showCount {
		area = append(area, g.Attr("data-textarea-counter-target", "input"))
		actions = append(actions, "input->textarea-counter#update")
	}
	if len(actions) > 0 {
		area = append(area, g.Attr("data-action", strings.Join(actions, " ")))
	}
	// HTML normalizes CR/LF and consumes the first LF after <textarea>.
	if strings.HasPrefix(p.Value, "\n") || strings.HasPrefix(p.Value, "\r") {
		area = append(area, g.Text("\n"))
	}
	area = append(area, g.Text(p.Value))

	rootClass := clFieldWrap
	if p.FullWidth {
		rootClass = clFieldWrapFull
	}
	field := []g.Node{
		h.Class(rootClass.Compile()),
		g.Attr("data-component", "textarea"),
	}
	if p.Hidden {
		field = append(field, g.Attr("hidden"))
	}
	if showCount {
		field = append(field, g.Attr("data-controller", "textarea-counter"))
	}
	if p.Label != "" {
		field = append(field, labelWithText(LabelProps{For: id, Required: p.Required}, g.Group{
			g.Raw("<!--pk-text:label-->"), g.Text(p.Label), g.Raw("<!--/pk-text:label-->"),
		}))
	}
	field = append(field, h.Textarea(area...))
	var supporting []g.Node
	if p.ErrorMessage != "" {
		errorAttrs := []g.Node{h.Class(clFieldErr.Compile()), h.Role("alert")}
		if id != "" {
			errorAttrs = append(errorAttrs, h.ID(id+"-error"))
		}
		supporting = append(supporting, h.P(append(errorAttrs, g.Text(p.ErrorMessage))...))
	}
	if p.HelperText != "" {
		helperAttrs := []g.Node{h.Class(clHelp.Compile())}
		if id != "" {
			helperAttrs = append(helperAttrs, h.ID(id+"-helper"))
		}
		supporting = append(supporting, h.P(append(helperAttrs, g.Text(p.HelperText))...))
	}
	var supportingNode g.Node
	if len(supporting) == 1 {
		supportingNode = supporting[0]
	} else if len(supporting) > 1 {
		supportingNode = h.Div(h.Class(clTextareaSupporting.Compile()), g.Group(supporting))
	}
	if showCount {
		current := utf8.RuneCountInString(p.Value)
		count := itoa(current)
		if p.MaxLength > 0 {
			count += " / " + itoa(p.MaxLength)
		}
		field = append(field, h.Div(
			h.Class(clTextareaMeta.Compile()),
			supportingNode,
			h.Span(
				h.Class(clTextareaCounter.Compile()),
				g.Attr("data-textarea-counter-target", "display"),
				g.Attr("aria-live", "polite"),
				g.Attr("aria-atomic", "true"),
				g.Text(count),
			),
		))
	} else if supportingNode != nil {
		field = append(field, supportingNode)
	}
	return h.Div(field...)
}
