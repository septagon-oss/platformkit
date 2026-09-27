package components

// text.go renders body text and normalizes every appearance Props it takes, so
// an unknown size or tone falls back rather than emitting a class no rule styles.

import (
	"strconv"
	"strings"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/ui/style"
)

// Text renders non-heading body copy with an allow-listed semantic element.
// Heading levels remain owned by Heading so document hierarchy cannot be
// smuggled through an untyped tag string.
func Text(p TextProps) g.Node {
	return textWithContent(p, g.Group{
		g.Raw("<!--pk-text:content-->"), g.Text(p.Content), g.Raw("<!--/pk-text:content-->"),
	}, g.Attr("data-component", "text"))
}

// Composing constructors can annotate their own property without replacing
// Text's semantics or styling. Only the public constructor marks a Text boundary.
func textWithContent(p TextProps, content g.Node, attrs ...g.Node) g.Node {
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
	children = append(children, baseAttrs(p.ComponentProps, attrs...)...)
	children = append(children,
		classes(cl.Compile(), p.Class),
		g.Attr("data-element", element),
		g.Attr("data-size", size),
		g.Attr("data-align", align),
		g.Attr("data-weight", weight),
		g.Attr("data-color", color),
	)
	if lines > 0 {
		children = append(children, g.Attr("data-lines", strconv.Itoa(lines)))
	}
	children = append(children, content)
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
