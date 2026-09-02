package components

// go renders the atom  Each renderer takes its Props struct
// and returns a gomponents Node; unknown variant or size strings fall back to
// the documented defaults rather than failing, matching the contracts'
// "data schema, not behavior" stance.

import (
	"strconv"
	"strings"
	"unicode/utf8"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/ui/icon"
	"github.com/septagon-oss/platformkit/ui/style"
)

func variantOr(m map[string]style.ClassList, key, fallback string) style.ClassList {
	if cl, ok := m[key]; ok {
		return cl
	}
	return m[fallback]
}

// Icon renders the OSS provider's vector directly into the document. Product
// and client providers extend the glyph vocabulary behind icon.Resolve while
// this atom retains sizing, semantic tone, and accessibility ownership.
func Icon(p IconProps) g.Node {
	size := p.Size
	if size == "" {
		size = "md"
	}
	tone := p.Tone
	if tone == "" {
		tone = "neutral"
	}
	cl := clIcon.
		Merge(variantOr(clIconSize, size, "md")).
		Merge(variantOr(clIconTone, tone, "neutral"))
	glyph, known := icon.Resolve(p.Name)
	nodes := baseAttrs(p.ComponentProps)
	nodes = append(
		nodes,
		classes(cl.Compile(), p.Class),
		g.Attr("xmlns", "http://www.w3.org/2000/svg"),
		g.Attr("viewBox", icon.ViewBox),
		g.Attr("fill", "currentColor"),
		g.Attr("focusable", "false"),
		g.Attr("data-pk-icon", p.Name),
	)
	if !known {
		nodes = append(nodes, g.Attr("data-pk-icon-fallback", "true"))
	}
	if p.AriaLabel == "" {
		nodes = append(nodes, g.Attr("aria-hidden", "true"))
	} else {
		nodes = append(nodes, g.Attr("role", "img"), g.Attr("aria-label", p.AriaLabel))
	}
	nodes = append(nodes, g.Raw(glyph.Body))
	return g.El("svg", nodes...)
}

// Button renders ButtonProps without trusted Go-composed slots.
func Button(p ButtonProps) g.Node {
	return ButtonWithSlots(p, ButtonSlots{})
}

// ButtonSlots carries trusted Go-composed content. Portable delivery graphs
// expose iconStart and iconEnd; Content is reserved for direct composition of
// compound controls whose accessible name is supplied through props.
type ButtonSlots struct {
	IconStart []g.Node
	IconEnd   []g.Node
	Content   []g.Node
}

// ButtonWithSlots renders a native button or, when Href is set, an anchor with
// button styling and link semantics. The two modes intentionally share one
// appearance, accessibility, HTMX, and state implementation.
func ButtonWithSlots(p ButtonProps, slots ButtonSlots) g.Node {
	variant := strings.ToLower(strings.TrimSpace(p.Variant))
	if _, exists := clButtonVariant[variant]; !exists {
		variant = "primary"
	}
	appearance := variantOr(clButtonVariant, variant, "primary")
	tone := strings.ToLower(strings.TrimSpace(p.Tone))
	if _, exists := clButtonTone[tone]; !exists {
		tone = "neutral"
	}
	if tone != "neutral" {
		appearance = clButtonTone[tone]
	}
	size := strings.ToLower(strings.TrimSpace(p.Size))
	if _, exists := clButtonSize[size]; !exists {
		size = "md"
	}
	cl := clButtonBase.
		Merge(appearance).
		Merge(clButtonSize[size])
	if p.FullWidth {
		cl = cl.Merge(clButtonFull)
	}
	if p.IconOnly {
		cl = cl.Merge(clButtonIconOnly)
	}
	if p.Href != "" && p.Disabled {
		cl = cl.Merge(clButtonDisabledLink)
	}
	typ := p.Type
	switch typ {
	case "button", "submit", "reset":
	default:
		typ = "button"
	}
	componentProps := p.ComponentProps
	if p.Href != "" {
		// disabled is not a valid anchor attribute; link mode emits the
		// equivalent accessible state below.
		componentProps.Disabled = false
	}
	var children []g.Node
	children = append(children, baseAttrs(componentProps, htmxAttrs(p.HTMXProps)...)...)
	children = append(children,
		classes(cl.Compile(), p.Class),
		g.Attr("data-component", "button"),
		g.Attr("data-variant", variant),
		g.Attr("data-tone", tone),
	)
	if label := p.AriaLabel; label != "" {
		children = append(children, g.Attr("aria-label", label))
	} else if p.IconOnly && p.Label != "" {
		children = append(children, g.Attr("aria-label", p.Label))
	}
	if p.Loading {
		children = append(children,
			g.Attr("data-loading", "true"),
			g.Attr("aria-busy", "true"),
		)
	}
	if len(slots.Content) > 0 {
		children = append(children, slots.Content...)
	} else if p.Loading {
		indicatorAppearance := variantOr(clButtonLoadingVariant, variant, "primary")
		if tone != "neutral" {
			indicatorAppearance = variantOr(clButtonLoadingTone, tone, "brand")
		}
		children = append(children, spinnerWithAppearance(
			SpinnerProps{Size: "sm", Label: ""},
			indicatorAppearance,
		))
	} else if len(slots.IconStart) > 0 {
		children = append(children, slots.IconStart...)
	}
	if len(slots.Content) == 0 && !p.IconOnly {
		children = append(children, g.Text(p.Label))
	}
	if len(slots.Content) == 0 && !p.Loading {
		if len(slots.IconEnd) > 0 {
			children = append(children, slots.IconEnd...)
		}
	}
	if p.Href != "" {
		children = append(children,
			h.Href(p.Href),
			g.Attr("data-button-as-link", "true"),
		)
		if p.Disabled {
			children = append(children,
				g.Attr("aria-disabled", "true"),
				g.Attr("tabindex", "-1"),
			)
		}
		return h.A(children...)
	}
	children = append(children, h.Type(typ))
	return h.Button(children...)
}

// Badge renders BadgeProps without adornment slots.
func Badge(p BadgeProps) g.Node {
	return BadgeWithSlots(p, BadgeSlots{})
}

// BadgeSlots carries trusted Go-composed adornments. Portable delivery graphs
// use the equivalent named iconStart and iconEnd slots.
type BadgeSlots struct {
	IconStart []g.Node
	IconEnd   []g.Node
}

// BadgeWithSlots renders a badge with optional leading and trailing content.
func BadgeWithSlots(p BadgeProps, slots BadgeSlots) g.Node {
	variant := strings.ToLower(strings.TrimSpace(p.Variant))
	if _, exists := clBadgeVariant[variant]; !exists {
		variant = "primary"
	}
	tone := strings.ToLower(strings.TrimSpace(p.Tone))
	if _, exists := clBadgeTone[tone]; !exists {
		tone = "neutral"
	}
	size := strings.ToLower(strings.TrimSpace(p.Size))
	if _, exists := clBadgeSize[size]; !exists {
		size = "md"
	}
	appearance := variantOr(clBadgeVariant, variant, "primary")
	if tone != "neutral" {
		appearance = variantOr(clBadgeTone, tone, "neutral")
	}
	cl := clBadgeBase.
		Merge(appearance).
		Merge(variantOr(clBadgeSize, size, "md"))
	var children []g.Node
	children = append(children, baseAttrs(p.ComponentProps)...)
	children = append(
		children,
		classes(cl.Compile(), p.Class),
		g.Attr("data-component", "badge"),
		g.Attr("data-variant", variant),
		g.Attr("data-tone", tone),
		g.Attr("data-size", size),
	)
	if p.Live {
		children = append(children, h.Role("status"), g.Attr("aria-live", "polite"))
	}
	if p.Dot {
		children = append(children, h.Span(
			h.Class(clBadgeDot.Merge(variantOr(clBadgeDotTone, tone, "neutral")).Compile()),
			g.Attr("aria-hidden", "true"),
			g.Attr("data-badge-dot", "true"),
		))
	}
	children = append(children, slots.IconStart...)
	children = append(children, g.Text(p.Label))
	if p.Count > 0 {
		count := strconv.Itoa(p.Count)
		if p.Count > 99 {
			count = "99+"
		}
		children = append(children, h.Span(
			h.Class(clBadgeCount.Compile()),
			g.Attr("data-badge-count", "true"),
			g.Text(count),
		))
	}
	children = append(children, slots.IconEnd...)
	if p.Removable {
		removeLabel := strings.TrimSpace(p.RemoveLabel)
		if removeLabel == "" {
			removeLabel = "Remove"
			if strings.TrimSpace(p.Label) != "" {
				removeLabel += " " + strings.TrimSpace(p.Label)
			}
		}
		children = append(children, h.Button(
			h.Type("button"),
			h.Class(clBadgeRemove.Compile()),
			g.Attr("aria-label", removeLabel),
			g.Attr("data-badge-remove", "true"),
			Icon(IconProps{Name: "x-mark", Size: "xs"}),
		))
	}
	return h.Span(children...)
}

// Alert renders AlertProps with role="alert" for danger/warning and
// role="status" otherwise, so severity maps to interruption behavior.
func Alert(p AlertProps) g.Node {
	return alertWithSlots(p, nil, nil)
}

func alertWithSlots(
	p AlertProps,
	iconStart []g.Node,
	actions []g.Node,
) g.Node {
	tone := p.Tone
	if tone == "" {
		tone = "info"
	}
	cl := clAlertBase.Merge(variantOr(clAlertVariant, tone, "info"))
	if p.Compact {
		cl = cl.Merge(clAlertCompact)
	} else {
		cl = cl.Merge(clAlertRegular)
	}
	if p.Bordered {
		cl = cl.Merge(clAlertBordered)
	}
	role := "status"
	live := "polite"
	if tone == "danger" || tone == "warning" {
		role = "alert"
		live = "assertive"
	}
	body := []g.Node{h.Class(clAlertBody.Compile())}
	if p.Title != "" {
		body = append(body, h.P(h.Class(clAlertTitle.Compile()), g.Text(p.Title)))
	}
	body = append(body, h.P(h.Class(clAlertMessage.Compile()), g.Text(p.Message)))

	var children []g.Node
	children = append(children, baseAttrs(p.ComponentProps)...)
	children = append(
		children,
		classes(cl.Compile(), p.Class),
		h.Role(role),
		g.Attr("aria-live", live),
		g.Attr("aria-atomic", "true"),
		g.Attr("data-component", "alert"),
		g.Attr("data-alert-tone", tone),
	)
	if p.Dismissible {
		children = append(
			children,
			g.Attr("data-controller", "alert"),
			g.Attr("data-alert-dismissible-value", "true"),
		)
	}
	if len(iconStart) == 0 {
		iconStart = []g.Node{Icon(IconProps{
			Name: defaultAlertIcon(tone),
			Size: "sm",
			Tone: tone,
		})}
	}
	if len(iconStart) > 0 {
		children = append(children, h.Span(
			h.Class(clAlertIcon.Compile()),
			g.Attr("data-alert-icon", ""),
			g.Group(iconStart),
		))
	}
	children = append(children, h.Div(body...))
	if len(actions) > 0 {
		children = append(children, h.Div(
			h.Class(clAlertActions.Compile()),
			g.Attr("data-alert-actions", ""),
			g.Group(actions),
		))
	}
	if p.Dismissible {
		children = append(children, h.Button(
			h.Type("button"),
			h.Class(clAlertClose.Compile()),
			g.Attr("data-action", "click->alert#dismiss"),
			g.Attr("data-alert-close", ""),
			g.Attr("aria-label", "Dismiss notification"),
			glyph("x-mark"),
		))
	}
	return h.Div(children...)
}

func defaultAlertIcon(tone string) string {
	switch tone {
	case "success":
		return "check-circle"
	case "warning":
		return "exclamation-triangle"
	case "danger":
		return "x-circle"
	default:
		return "information-circle"
	}
}

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

	input := []g.Node{
		classes(cl.Compile(), p.Class),
		h.ID(id), h.Name(p.Name), h.Type(typ),
		g.Attr("data-tone", tone),
		g.Attr("data-size", size),
	}
	input = append(input, attrPairs(p.Attrs)...)
	input = append(input, htmxAttrs(p.HTMXProps)...)
	if p.Value != "" {
		input = append(input, h.Value(p.Value))
	}
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
	describedBy := make([]string, 0, 2)
	if invalid {
		input = append(input, g.Attr("aria-invalid", "true"))
	}
	if p.Error != "" {
		describedBy = append(describedBy, id+"-error")
	}
	if p.HelpText != "" {
		describedBy = append(describedBy, id+"-help")
	}
	if len(describedBy) > 0 {
		input = append(input, g.Attr("aria-describedby", strings.Join(describedBy, " ")))
	}
	if p.Label == "" && p.Name != "" {
		input = append(input, g.Attr("aria-label", p.Name))
	}

	fieldClass := clFieldWrap
	if p.FullWidth {
		fieldClass = fieldClass.Merge(clFieldWrapFull)
	}
	field := []g.Node{h.Class(fieldClass.Compile()), g.Attr("data-component", componentName)}
	if p.Label != "" {
		field = append(field, Label(LabelProps{Text: p.Label, For: id, Required: p.Required}))
	}
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
		field = append(field, h.Div(controlChildren...))
	} else {
		field = append(field, control)
	}
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

func canonicalInputType(raw string) (string, bool) {
	typ := strings.ToLower(strings.TrimSpace(raw))
	if typ == "" {
		return "text", true
	}
	switch typ {
	case "text", "email", "password", "number", "tel", "url", "search",
		"date", "time", "datetime-local", "month", "week", "color", "hidden":
		return typ, true
	default:
		return typ, false
	}
}

// Select renders SelectProps as a labelled native <select>, styled as
// an input-family control. A Placeholder renders as an empty leading option
// so an untouched control has no accidental value; when the field is
// Required the placeholder is how "nothing chosen yet" stays expressible.
func Select(p SelectProps) g.Node {
	id := p.ID
	if id == "" && p.Name != "" {
		id = "pk-select-" + p.Name
	}
	cl := clInput.Merge(clInputNormal).Merge(variantOr(clInputSize, "md", "md"))
	if p.Error != "" {
		cl = clInput.Merge(clInputError).Merge(variantOr(clInputSize, "md", "md"))
	}

	selectedValues := make(map[string]struct{}, len(p.Values)+1)
	for _, value := range p.Values {
		value = strings.TrimSpace(value)
		if value != "" {
			selectedValues[value] = struct{}{}
		}
	}
	if value := strings.TrimSpace(p.Value); value != "" {
		selectedValues[value] = struct{}{}
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
		fieldClass = fieldClass.Merge(clFieldWrapFull)
	}
	field := []g.Node{
		h.Class(fieldClass.Compile()),
		g.Attr("data-component", "select"),
	}
	if p.Label != "" {
		field = append(field, Label(LabelProps{Text: p.Label, For: id, Required: p.Required}))
	}
	field = append(field, h.Select(sel...))
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
	area = append(area, g.Text(p.Value))

	rootClass := clFieldWrap
	if p.FullWidth {
		rootClass = rootClass.Merge(clTextareaFull)
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
		field = append(field, Label(LabelProps{Text: p.Label, For: id, Required: p.Required}))
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
	if p.Indeterminate {
		box = append(box, g.Attr("aria-checked", "mixed"))
	}
	if p.Label == "" && p.Name != "" {
		box = append(box, g.Attr("aria-label", p.Name))
	}
	if p.HelpText != "" {
		box = append(box, g.Attr("aria-describedby", id+"-help"))
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
	if p.HelpText == "" {
		return control
	}
	return h.Div(
		h.Class(clFieldWrap.Compile()),
		control,
		h.P(h.ID(id+"-help"), h.Class(clHelp.Compile()), g.Text(p.HelpText)),
	)
}

// Label renders LabelProps; required fields carry a visible marker the
// screen reader skips (the input's required attribute carries the semantics).
func Label(p LabelProps) g.Node {
	children := []g.Node{h.For(p.For), classes(clLabel.Compile(), p.Class), g.Text(p.Text)}
	if p.Required {
		children = append(children, h.Span(
			h.Class(clRequired.Compile()), g.Attr("aria-hidden", "true"), g.Text(" *"),
		))
	}
	return h.Label(children...)
}

// Text renders non-heading body copy with an allow-listed semantic element.
// Heading levels remain owned by Heading so document hierarchy cannot be
// smuggled through an untyped tag string.
func Text(p TextProps) g.Node {
	element := normalizeTextElement(p.Element)
	size := normalizeTextSize(p.Size)
	align := normalizeTextAlign(p.Align)
	weight := normalizeTextWeight(p.Weight)
	color := normalizeTextColor(p.Color)
	transform := normalizeTextTransform(p.Transform)

	cl := style.New()
	cl = cl.
		FontSize(clTextSize[size]).
		TextAlign(clTextAlign[align]).
		FontWeight(clTextWeight[weight]).
		TextColor(clTextColor[color]).
		Merge(clTextTransform[transform])
	if p.Italic {
		cl = cl.Merge(clTextItalic)
	}
	if p.Underline {
		cl = cl.Merge(clTextUnderline)
	}
	lines := p.Lines
	if lines < 0 {
		lines = 0
	}
	if lines > 6 {
		lines = 6
	}
	if lines > 0 {
		cl = cl.LineClamp(lines)
	} else if p.Truncate {
		cl = cl.Merge(clTruncate)
	}
	if p.NoWrap {
		cl = cl.Merge(clTextNoWrap)
	}
	var children []g.Node
	children = append(children, baseAttrs(p.ComponentProps)...)
	children = append(children,
		classes(cl.Compile(), p.Class),
		g.Attr("data-component", "text"),
		g.Attr("data-element", element),
		g.Attr("data-size", size),
		g.Attr("data-align", align),
		g.Attr("data-weight", weight),
		g.Attr("data-color", color),
	)
	if lines > 0 {
		children = append(children, g.Attr("data-lines", strconv.Itoa(lines)))
	}
	children = append(children, g.Text(p.Content))
	return g.El(element, children...)
}

func normalizeTextElement(element string) string {
	switch strings.ToLower(strings.TrimSpace(element)) {
	case "span", "div", "strong", "em", "small", "mark", "del", "ins",
		"sub", "sup", "blockquote", "code", "pre", "kbd", "samp", "var":
		return strings.ToLower(strings.TrimSpace(element))
	default:
		return "p"
	}
}

func normalizeTextSize(size string) string {
	switch strings.ToLower(strings.TrimSpace(size)) {
	case "xs", "sm", "lg", "xl", "2xl", "3xl", "4xl", "5xl":
		return strings.ToLower(strings.TrimSpace(size))
	default:
		return "base"
	}
}

func normalizeTextAlign(align string) string {
	switch strings.ToLower(strings.TrimSpace(align)) {
	case "center", "right", "justify":
		return strings.ToLower(strings.TrimSpace(align))
	default:
		return "left"
	}
}

func normalizeTextWeight(weight string) string {
	weight = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(weight), " ", ""))
	switch weight {
	case "thin", "extralight", "light", "medium", "semibold", "bold", "extrabold", "black":
		return weight
	default:
		return "normal"
	}
}

func normalizeTextColor(color string) string {
	color = strings.ToLower(strings.TrimSpace(color))
	if _, exists := clTextColor[color]; exists {
		return color
	}
	return "primary"
}

func normalizeTextTransform(transform string) string {
	switch strings.ToLower(strings.TrimSpace(transform)) {
	case "uppercase", "lowercase", "capitalize":
		return strings.ToLower(strings.TrimSpace(transform))
	default:
		return "none"
	}
}

// Heading renders HeadingProps at the given level (clamped 1..6) in the
// design system's display face.
func Heading(p HeadingProps) g.Node {
	level := p.Level
	if level < 1 || level > 6 {
		level = 2
	}
	cl := clHeadingBase.Merge(clHeadingLevel[level])
	if p.Truncate {
		cl = cl.Merge(clTruncate)
	}
	var children []g.Node
	children = append(children, baseAttrs(p.ComponentProps)...)
	if p.Anchor != "" {
		children = append(children, h.ID(p.Anchor))
	}
	children = append(children, classes(cl.Compile(), p.Class), g.Text(p.Text))
	switch level {
	case 1:
		return h.H1(children...)
	case 3:
		return h.H3(children...)
	case 4:
		return h.H4(children...)
	case 5:
		return h.H5(children...)
	case 6:
		return h.H6(children...)
	default:
		return h.H2(children...)
	}
}

// Divider renders DividerProps as an <hr>, a labelled horizontal
// separator, or a vertical separator.
func Divider(p DividerProps) g.Node {
	if p.Orientation == "vertical" {
		return h.Span(baseAttrs(
			p.ComponentProps,
			classes(clDividerV.Compile(), p.Class),
			h.Role("separator"), g.Attr("aria-orientation", "vertical"),
		)...)
	}
	if p.Text != "" {
		children := baseAttrs(
			p.ComponentProps,
			classes(clDividerText.Compile(), p.Class),
			h.Role("presentation"),
		)
		children = append(
			children,
			h.Span(
				classes(clDividerTextLine.Compile(), ""),
				g.Attr("aria-hidden", "true"),
			),
			h.Span(classes(clDividerTextLabel.Compile(), ""), g.Text(p.Text)),
			h.Span(
				classes(clDividerTextLine.Compile(), ""),
				g.Attr("aria-hidden", "true"),
			),
		)
		return h.Div(children...)
	}
	return h.Hr(baseAttrs(
		p.ComponentProps,
		classes(clDividerH.Compile(), p.Class),
		h.Role("separator"),
		g.Attr("aria-orientation", "horizontal"),
	)...)
}

// Spinner renders SpinnerProps; the label is announced, the rotation is
// decoration.
func Spinner(p SpinnerProps) g.Node {
	return spinnerWithAppearance(
		p,
		variantOr(clSpinnerTone, normalizeSpinnerTone(p.Tone), "brand"),
	)
}

func spinnerWithAppearance(p SpinnerProps, appearance style.ClassList) g.Node {
	cl := clSpinner.
		Merge(variantOr(clSpinnerSize, p.Size, "md")).
		Merge(appearance)
	label := p.Label
	if label == "" {
		label = "Loading"
	}
	return h.Span(
		classes(cl.Compile(), p.Class),
		h.Role("status"), g.Attr("aria-label", label),
	)
}

func normalizeSpinnerTone(tone string) string {
	switch strings.ToLower(strings.TrimSpace(tone)) {
	case "neutral", "success", "warning", "danger", "info", "brand":
		return strings.ToLower(strings.TrimSpace(tone))
	default:
		return "brand"
	}
}

// EmptyState renders EmptyStateProps.
func EmptyState(p EmptyStateProps) g.Node {
	return emptyStateWithSlots(p, nil, nil)
}

func emptyStateWithSlots(
	p EmptyStateProps,
	iconStart []g.Node,
	actions []g.Node,
) g.Node {
	cl := clEmpty.Merge(clEmptyPad)
	if p.Compact {
		cl = clEmpty.Merge(clEmptyCompact)
	}
	if p.Bordered {
		cl = cl.Merge(clEmptyBordered)
	}
	var children []g.Node
	children = append(children, baseAttrs(p.ComponentProps)...)
	children = append(children, classes(cl.Compile(), p.Class))
	children = append(children, iconStart...)
	children = append(children, h.P(h.Class(clEmptyTitle.Compile()), g.Text(p.Title)))
	if p.Description != "" {
		children = append(children, h.P(h.Class(clEmptyDesc.Compile()), g.Text(p.Description)))
	}
	children = append(children, actions...)
	return h.Div(children...)
}

// Link renders LinkProps; external links open safely.
func Link(p LinkProps) g.Node {
	return linkWithSlots(p, nil)
}

func linkWithSlots(
	p LinkProps,
	trailingAdornment []g.Node,
) g.Node {
	var children []g.Node
	children = append(children, baseAttrs(p.ComponentProps, htmxAttrs(p.HTMXProps)...)...)
	children = append(children, classes(clLink.Compile(), p.Class), h.Href(p.Href))
	target, rel := p.Target, p.Rel
	if p.External {
		if target == "" {
			target = "_blank"
		}
		if rel == "" {
			rel = "noopener noreferrer"
		}
	}
	if target != "" {
		children = append(children, h.Target(target))
	}
	if rel != "" {
		children = append(children, h.Rel(rel))
	}
	children = append(children, g.Text(p.Label))
	children = append(children, trailingAdornment...)
	if p.External {
		children = append(children, h.Span(g.Attr("aria-hidden", "true"), g.Text(" ↗")))
	}
	return h.A(children...)
}
