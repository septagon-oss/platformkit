package components

// link.go renders the anchor and its slot form.

import (
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

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
