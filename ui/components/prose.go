package components

import (
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/ui/css"
	"github.com/septagon-oss/platformkit/ui/style"
)

// ProseProps carries HTML already sanitized by kit/richtext.Render.
type ProseProps struct{ HTML string }

// Prose renders sanitized Markdown HTML within the shared prose column.
// Callers must pass only the result of kit/richtext.Render.
func Prose(p ProseProps) g.Node {
	return h.Div(g.Attr("data-component", "prose"), g.Attr("data-prose", ""), h.Class(clProse.Compile()), g.Raw(p.HTML))
}

// ProseStyle is the component's token-only rules for web and generated screens.
func ProseStyle() *css.Sheet {
	lit, ref := css.Literal, func(name string) css.Value { return css.VarRef(name, "") }
	size := func(f style.FontSize) css.Value { return lit(f.Value()) }
	step := func(s style.Spacing) css.Value { return lit(s.Value()) }
	below := func(s style.Spacing) css.Value { return lit("0 0 " + s.Value()) }
	around := func(above, under style.Spacing) css.Value { return lit(above.Value() + " 0 " + under.Value()) }
	rule := func(w style.BorderWidth) css.Value { return lit(w.Value() + " solid") }
	s := css.NewSheet()
	s.Select("[data-prose]", css.Decl("line-height", lit("1.7")))
	s.Select("[data-prose] h1", css.Decl("font-size", size(style.Text3XL)), css.Decl("line-height", lit("1.2")), css.Decl("margin", below(style.S4)))
	s.Select("[data-prose] h2", css.Decl("font-size", size(style.Text2XL)), css.Decl("line-height", lit("1.3")), css.Decl("margin", around(style.S8, style.S3)))
	s.Select("[data-prose] h3", css.Decl("font-size", size(style.TextXL)), css.Decl("margin", around(style.S6, style.S2)))
	s.Select("[data-prose] h4", css.Decl("font-size", size(style.TextLG)), css.Decl("margin", around(style.S4, style.S2)))
	s.Select("[data-prose] p, [data-prose] ul, [data-prose] ol, [data-prose] blockquote, [data-prose] pre, [data-prose] table, [data-prose] figure", css.Decl("margin", below(style.S4)))
	s.Select("[data-prose] ul, [data-prose] ol", css.Decl("padding-left", step(style.S6)))
	s.Select("[data-prose] ul", css.Decl("list-style", lit("disc")))
	s.Select("[data-prose] ol", css.Decl("list-style", lit("decimal")))
	s.Select("[data-prose] a", css.Decl("color", ref("pk-color-text-primary")), css.Decl("text-decoration", lit("underline")))
	s.Select("[data-prose] blockquote", css.Decl("border-left", rule(style.Border4)), css.Decl("border-color", ref("pk-color-border-strong")), css.Decl("padding-left", step(style.S4)), css.Decl("color", ref("pk-color-text-muted")))
	s.Select("[data-prose] pre", css.Decl("padding", step(style.S4)), css.Decl("overflow-x", lit("auto")), css.Decl("border-radius", lit(style.RadiusLG.Value())), css.Decl("background", ref("pk-color-surface-muted")))
	s.Select("[data-prose] code", css.Decl("font-family", ref("pk-font-mono")), css.Decl("font-size", lit("0.925em")))
	s.Select("[data-prose] img", css.Decl("max-width", step(style.SFull)), css.Decl("height", lit("auto")))
	s.Select("[data-prose] figcaption", css.Decl("font-size", size(style.TextSM)), css.Decl("color", ref("pk-color-text-muted")))
	s.Select("[data-prose] table", css.Decl("border-collapse", lit("collapse")), css.Decl("width", step(style.SFull)), css.Decl("display", lit("block")), css.Decl("overflow-x", lit("auto")))
	s.Select("[data-prose] th, [data-prose] td", css.Decl("border", rule(style.Border1)), css.Decl("border-color", ref("pk-color-border-default")), css.Decl("padding", step(style.S2)), css.Decl("text-align", lit("left")))
	s.Select("[data-prose] h2", css.Decl("scroll-margin-top", step(style.S8)))
	s.Select("[data-prose] h3", css.Decl("scroll-margin-top", step(style.S8)))
	s.Select("[data-prose] h4", css.Decl("scroll-margin-top", step(style.S8)))
	return s
}
