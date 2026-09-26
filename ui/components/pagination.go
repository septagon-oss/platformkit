package components

// pagination.go renders the page controls, their labels and the URL each page
// addresses, with or without the progressive enhancement.

import (
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"net/url"
)

// Pagination renders PaginationProps as previous/next plus a
// sibling window around the current page. Page links append ?page=N to
// BaseURL; when HTMX props are set they ride along on every link.
func Pagination(p PaginationProps) g.Node {
	if p.TotalPages <= 1 {
		return g.Text("")
	}
	siblings := p.Siblings
	if siblings <= 0 {
		siblings = 1
	}
	pageHref := func(n int) string {
		return paginationPageURL(p.BaseURL, n)
	}
	pageLink := func(n int, label string, current bool, ariaLabel, marker string) g.Node {
		cl := clPageBtn.Merge(clPageIdle)
		if current {
			cl = clPageBtn.Merge(clPageCur)
		}
		nodes := []g.Node{
			h.Class(cl.Compile()),
			h.Href(pageHref(n)),
			g.Attr("data-page", itoa(n)),
		}
		if marker != "" {
			nodes = append(nodes, g.Attr(marker, ""))
		}
		if current {
			nodes = append(nodes, g.Attr("aria-current", "page"))
		}
		if ariaLabel != "" {
			nodes = append(nodes, g.Attr("aria-label", ariaLabel))
		}
		if !current {
			enhancement := p.HTMXProps
			if hasHTMXEnhancement(enhancement) {
				enhancement.Get = pageHref(n)
			}
			nodes = append(nodes, htmxAttrs(enhancement)...)
		}
		return h.A(append(nodes, g.Text(label))...)
	}
	disabledBoundary := func(label, marker, glyph string) g.Node {
		return h.Button(
			h.Class(clPageBtn.Merge(clPageIdle).Compile()),
			h.Type("button"),
			g.Attr(marker, ""),
			g.Attr("aria-label", label),
			h.Disabled(),
			g.Text(glyph),
		)
	}

	previous := fallbackText(strings.TrimSpace(p.PreviousLabel), "Previous page")
	following := fallbackText(strings.TrimSpace(p.NextLabel), "Next page")
	numbered := func(n int) string { return pageLabel(fallbackText(p.PageLabel, "Go to page %d"), n) }
	current := func(n int) string { return pageLabel(fallbackText(p.CurrentPageLabel, "Page %d, current page"), n) }

	items := []g.Node{h.Class(clPagination.Compile())}
	if p.CurrentPage > 1 {
		items = append(items, pageLink(p.CurrentPage-1, "‹", false, previous, "data-pagination-prev"))
	} else {
		items = append(items, disabledBoundary(previous, "data-pagination-prev", "‹"))
	}
	lo, hi := p.CurrentPage-siblings, p.CurrentPage+siblings
	if lo < 1 {
		lo = 1
	}
	if hi > p.TotalPages {
		hi = p.TotalPages
	}
	if lo > 1 {
		items = append(items, pageLink(1, "1", p.CurrentPage == 1, numbered(1), ""))
		if lo > 2 {
			items = append(items, h.Span(h.Class(clBreadcrumbSep.Compile()), g.Text("…")))
		}
	}
	for n := lo; n <= hi; n++ {
		ariaLabel := numbered(n)
		if n == p.CurrentPage {
			ariaLabel = current(n)
		}
		items = append(items, pageLink(n, itoa(n), n == p.CurrentPage, ariaLabel, ""))
	}
	if hi < p.TotalPages {
		if hi < p.TotalPages-1 {
			items = append(items, h.Span(h.Class(clBreadcrumbSep.Compile()), g.Text("…")))
		}
		items = append(items, pageLink(p.TotalPages, itoa(p.TotalPages), false, numbered(p.TotalPages), ""))
	}
	if p.CurrentPage < p.TotalPages {
		items = append(items, pageLink(p.CurrentPage+1, "›", false, following, "data-pagination-next"))
	} else {
		items = append(items, disabledBoundary(following, "data-pagination-next", "›"))
	}

	nav := baseAttrs(p.ComponentProps)
	nav = append(nav, g.Attr("data-component", "pagination"),
		g.Attr("aria-label", fallbackText(strings.TrimSpace(p.NavigationLabel), "Pagination")))
	nav = append(nav, items...)
	return h.Nav(nav...)
}

// pageLabel puts the page number where the label says. It substitutes rather
// than formats so a label without the marker renders as written instead of
// as a fmt diagnostic: Props are data, and data does not fail a render.
func pageLabel(format string, n int) string {
	return strings.Replace(format, "%d", itoa(n), 1)
}

func paginationPageURL(baseURL string, page int) string {
	parsed, err := url.Parse(baseURL)
	if err == nil {
		query := parsed.Query()
		query.Set("page", itoa(page))
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}

	separator := "?"
	if strings.Contains(baseURL, "?") {
		separator = "&"
	}
	return baseURL + separator + "page=" + itoa(page)
}

func hasHTMXEnhancement(p HTMXProps) bool {
	return p.Get != "" || p.Post != "" || p.Put != "" || p.Patch != "" ||
		p.Delete != "" || p.Target != "" || p.Swap != "" || p.Trigger != "" ||
		p.Confirm != "" || p.Ext != "" || p.Indicator != "" ||
		p.DisabledElt != "" || p.Vals != "" || p.PushURL != "" ||
		p.Select != "" || p.Boost || p.Disable
}
