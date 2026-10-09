package components

// detail_list.go renders term-and-definition pairs, in the semantic role the
// caller names and no other.

import (
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// DetailListSlots is the trusted Go composition seam for a value that is more
// than a string — an instant that travels with its machine form, for one. It
// mirrors TableSlots one-for-one: the data stays in DetailListProps, and a
// caller opts into a callback only when the value needs real markup.
type DetailListSlots struct {
	// Value returns the markup for one item's value. Nil, or nil for one item,
	// falls back to the item's own text, exactly as DetailList renders it.
	Value func(DetailItem) g.Node
}

// DetailList renders a governed group of label/value facts as a native
// description list. SemanticRole is intentionally projected only as a data
// attribute: it is an adaptive-surface machine key, not an HTML/ARIA role or
// translated label.
func DetailList(p DetailListProps) g.Node { return DetailListWithSlots(p, DetailListSlots{}) }

// DetailListWithSlots renders DetailListProps with one value door open.
func DetailListWithSlots(p DetailListProps, slots DetailListSlots) g.Node {
	rootProps := p.ComponentProps
	rootProps.Class = ""
	rootProps.Disabled = false
	root := baseAttrs(
		rootProps,
		classes(clDetailList.Compile(), p.Class),
		g.Attr("data-component", "detail-list"),
	)
	if p.Disabled {
		root = append(root, g.Attr("aria-disabled", "true"))
	}
	if semanticRole := validDetailSemanticRole(p.SemanticRole); semanticRole != "" {
		root = append(root, g.Attr("data-semantic-role", semanticRole))
	}

	headingID := ""
	descriptionID := ""
	hasTitle := strings.TrimSpace(p.Title) != ""
	hasDescription := strings.TrimSpace(p.Description) != ""
	if id := strings.TrimSpace(p.ID); id != "" {
		if hasTitle {
			headingID = id + "-title"
			root = append(root, g.Attr("aria-labelledby", headingID))
		}
		if hasDescription {
			descriptionID = id + "-description"
			root = append(root, g.Attr("aria-describedby", descriptionID))
		}
	}
	if hasTitle || hasDescription {
		header := []g.Node{h.Class(clDetailHeader.Compile())}
		if hasTitle {
			title := []g.Node{h.Class(clDetailTitle.Compile())}
			if headingID != "" {
				title = append(title, h.ID(headingID))
			}
			title = append(title, g.Text(p.Title))
			header = append(header, h.H2(title...))
		}
		if hasDescription {
			description := []g.Node{h.Class(clDetailDescription.Compile())}
			if descriptionID != "" {
				description = append(description, h.ID(descriptionID))
			}
			description = append(description, g.Text(p.Description))
			header = append(header, h.P(description...))
		}
		root = append(root, h.Header(header...))
	}

	items := make([]g.Node, 0, len(p.Items)+1)
	items = append(items, h.Class(clDetailItems.Compile()))
	validItems := 0
	for _, item := range p.Items {
		if strings.TrimSpace(item.Label) == "" || strings.TrimSpace(item.Value) == "" {
			continue
		}
		rowClass := clDetailRow
		if validItems > 0 {
			rowClass = rowClass.Merge(clDetailRowSeparated)
		}
		tone := strings.ToLower(strings.TrimSpace(item.Tone))
		if _, ok := clDetailValueTone[tone]; !ok {
			tone = "neutral"
		}
		term := []g.Node{h.Class(clDetailTerm.Compile()), g.Text(item.Label)}
		if strings.TrimSpace(item.Description) != "" {
			term = append(term, h.Span(
				h.Class(clDetailTermDescription.Compile()),
				g.Text(item.Description),
			))
		}
		value := h.Dd(
			h.Class(clDetailValue.Merge(clDetailValueTone[tone]).Compile()),
			g.Text(item.Value),
		)
		if slots.Value != nil {
			if custom := slots.Value(item); custom != nil {
				value = h.Dd(h.Class(clDetailValue.Merge(clDetailValueTone[tone]).Compile()), custom)
			}
		}
		items = append(items, h.Div(
			h.Class(rowClass.Compile()),
			g.Attr("data-detail-item", ""),
			h.Dt(term...),
			value,
		))
		validItems++
	}
	if validItems > 0 {
		root = append(root, h.Dl(items...))
	}
	return h.Section(root...)
}

// validDetailSemanticRole accepts portable machine keys while failing closed
// for display copy, whitespace, URLs, and other values that should never be
// used to drive renderer behavior. Recommended shared keys include identity,
// preferences, and activity; namespaced extensions remain possible.
func validDetailSemanticRole(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == 0 || len(value) > 64 {
		return ""
	}
	for index, char := range value {
		if index == 0 {
			if char < 'a' || char > 'z' {
				return ""
			}
			continue
		}
		if (char >= 'a' && char <= 'z') ||
			(char >= '0' && char <= '9') ||
			char == '-' || char == '_' || char == '.' {
			continue
		}
		return ""
	}
	return value
}
