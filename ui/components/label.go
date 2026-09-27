package components

// label.go renders the field label, the one place a control's name is authored.

import (
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// Label renders LabelProps; required fields carry a visible marker the
// screen reader skips (the input's required attribute carries the semantics).
func Label(p LabelProps) g.Node {
	return labelWithText(p, g.Group{
		g.Raw("<!--pk-text:text-->"), g.Text(p.Text), g.Raw("<!--/pk-text:text-->"),
	})
}

func labelWithText(p LabelProps, text g.Node) g.Node {
	children := []g.Node{h.For(p.For), classes(clLabel.Compile(), p.Class), text}
	if p.Required {
		children = append(children, h.Span(
			h.Class(clRequired.Compile()), g.Attr("aria-hidden", "true"), g.Text(" *"),
		))
	}
	return h.Label(children...)
}
