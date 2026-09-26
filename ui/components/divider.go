package components

// divider.go renders the rule between regions, decorative unless it is labelled.

import (
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

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
