package components

// breadcrumb.go renders the trail and collapses the middle of a long one.

import (
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// Breadcrumb renders BreadcrumbProps as an aria-labelled trail; the
// current page is text, not a link, and carries aria-current.
func Breadcrumb(p BreadcrumbProps) g.Node {
	if len(p.Items) == 0 {
		return nil
	}
	sep := p.Separator
	if sep == "" {
		sep = "/"
	}
	visible := breadcrumbItems(p.Items, p.MaxItems)
	items := []g.Node{h.Class(clBreadcrumb.Compile())}
	for i, it := range visible {
		if i > 0 {
			items = append(items, h.Li(
				h.Class(clBreadcrumbSep.Compile()), g.Attr("aria-hidden", "true"), g.Text(sep),
			))
		}
		if it.Label == "…" && it.Href == "" && !it.Current {
			items = append(items, h.Li(
				h.Class(clBreadcrumbSep.Compile()),
				g.Attr("aria-hidden", "true"),
				g.Attr("data-breadcrumb-ellipsis", ""),
				g.Text("…"),
			))
			continue
		}
		current := it.Current || (it.Href == "" && i == len(visible)-1)
		if current {
			content := []g.Node{g.Text(it.Label)}
			if it.Icon != "" {
				content = append([]g.Node{glyph(it.Icon)}, content...)
			}
			items = append(items, h.Li(append(
				[]g.Node{h.Class(clBreadcrumbCur.Compile()), g.Attr("aria-current", "page")},
				content...,
			)...))
			continue
		}
		var adornment []g.Node
		if it.Icon != "" {
			adornment = []g.Node{glyph(it.Icon)}
		}
		items = append(items, h.Li(linkWithSlots(
			LinkProps{Label: it.Label, Href: it.Href},
			adornment,
		)))
	}
	nav := baseAttrs(p.ComponentProps)
	if p.Class != "" {
		nav = append(nav, h.Class(p.Class))
	}
	nav = append(nav, htmxAttrs(p.HTMXProps)...)
	nav = append(nav,
		g.Attr("data-component", "breadcrumb"),
		g.Attr("aria-label", fallbackText(strings.TrimSpace(p.NavigationLabel), "Breadcrumb")),
		h.Ol(items...),
	)
	return h.Nav(nav...)
}

func breadcrumbItems(items []BreadcrumbItem, maxItems int) []BreadcrumbItem {
	if maxItems <= 0 || len(items) <= maxItems || maxItems < 2 {
		return items
	}

	tailCount := max(maxItems-1, 1)
	startOfTail := len(items) - tailCount
	visible := make([]BreadcrumbItem, 0, maxItems+1)
	visible = append(visible, items[0], BreadcrumbItem{Label: "…"})
	return append(visible, items[startOfTail:]...)
}
