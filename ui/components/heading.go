package components

// heading.go renders a heading at the level its caller states; the level is the
// document outline and never a size.

import (
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// Heading renders HeadingProps at the given level (clamped 1..6) in the
// design system's display face.
func Heading(p HeadingProps) g.Node {
	return headingWithText(p, g.Group{
		g.Raw("<!--pk-text:text-->"), g.Text(p.Text), g.Raw("<!--/pk-text:text-->"),
	})
}

func headingWithText(p HeadingProps, text g.Node) g.Node {
	level := p.Level
	if level < 1 || level > 6 {
		level = 2
	}
	size := p.Size
	if size < 1 || size > 6 {
		size = level
	}
	cl := clHeadingBase.Merge(clHeadingLevel[size])
	if p.Truncate {
		cl = cl.Merge(clTruncate)
	}
	var children []g.Node
	children = append(children, baseAttrs(p.ComponentProps)...)
	if p.Anchor != "" {
		children = append(children, h.ID(p.Anchor))
	}
	children = append(children, classes(cl.Compile(), p.Class), text)
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
