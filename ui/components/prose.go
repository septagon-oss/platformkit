package components

import (
	"strings"

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
	return h.Div(g.Attr("data-component", "prose"), h.Class(clProse.Compile()), g.Raw(p.HTML))
}

// ProseStyle is the component's token-only rules for web and generated screens.
func ProseStyle() *css.Sheet {
	return ProseStyleFor(`[data-component="prose"]`)
}

// ProseStyleFor applies the shared prose rules under a caller-owned hook.
// A caller placing these rules in the client layer must own that hook.
func ProseStyleFor(hook string) *css.Sheet {
	selector := func(s string) string { return strings.ReplaceAll(s, "&", hook) }
	lit, ref := css.Literal, func(name string) css.Value { return css.VarRef(name, "") }
	size := func(f style.FontSize) css.Value { return lit(f.Value()) }
	step := func(s style.Spacing) css.Value { return lit(s.Value()) }
	below := func(s style.Spacing) css.Value { return lit("0 0 " + s.Value()) }
	around := func(above, under style.Spacing) css.Value { return lit(above.Value() + " 0 " + under.Value()) }
	rule := func(w style.BorderWidth) css.Value { return lit(w.Value() + " solid") }
	s := css.NewSheet()
	s.Select(selector("&"), css.Decl("line-height", lit("1.7")))
	s.Select(selector("& h1"), css.Decl("font-size", size(style.Text3XL)), css.Decl("line-height", lit("1.2")), css.Decl("margin", below(style.S4)))
	s.Select(selector("& h2"), css.Decl("font-size", size(style.Text2XL)), css.Decl("line-height", lit("1.3")), css.Decl("margin", around(style.S8, style.S3)))
	s.Select(selector("& h3"), css.Decl("font-size", size(style.TextXL)), css.Decl("margin", around(style.S6, style.S2)))
	s.Select(selector("& h4"), css.Decl("font-size", size(style.TextLG)), css.Decl("margin", around(style.S4, style.S2)))
	s.Select(selector("& p, & ul, & ol, & blockquote, & pre, & table, & figure"), css.Decl("margin", below(style.S4)))
	s.Select(selector("& ul, & ol"), css.Decl("padding-left", step(style.S6)))
	s.Select(selector("& ul"), css.Decl("list-style", lit("disc")))
	s.Select(selector("& ol"), css.Decl("list-style", lit("decimal")))
	s.Select(selector("& a"), css.Decl("color", ref("pk-color-text-primary")), css.Decl("text-decoration", lit("underline")))
	s.Select(selector("& blockquote"), css.Decl("border-left", rule(style.Border4)), css.Decl("border-color", ref("pk-color-border-strong")), css.Decl("padding-left", step(style.S4)), css.Decl("color", ref("pk-color-text-muted")))
	s.Select(selector("& pre"), css.Decl("padding", step(style.S4)), css.Decl("overflow-x", lit("auto")), css.Decl("border-radius", lit(style.RadiusLG.Value())), css.Decl("background", ref("pk-color-surface-muted")))
	s.Select(selector("& code"), css.Decl("font-family", ref("pk-font-mono")), css.Decl("font-size", lit("0.925em")))
	s.Select(selector("& img"), css.Decl("max-width", step(style.SFull)), css.Decl("height", lit("auto")))
	s.Select(selector("& figcaption"), css.Decl("font-size", size(style.TextSM)), css.Decl("color", ref("pk-color-text-muted")))
	s.Select(selector("& table"), css.Decl("border-collapse", lit("collapse")), css.Decl("width", step(style.SFull)), css.Decl("display", lit("block")), css.Decl("overflow-x", lit("auto")))
	s.Select(selector("& th, & td"), css.Decl("border", rule(style.Border1)), css.Decl("border-color", ref("pk-color-border-default")), css.Decl("padding", step(style.S2)), css.Decl("text-align", lit("left")))
	s.Select(selector("& h2"), css.Decl("scroll-margin-top", step(style.S8)))
	s.Select(selector("& h3"), css.Decl("scroll-margin-top", step(style.S8)))
	s.Select(selector("& h4"), css.Decl("scroll-margin-top", step(style.S8)))
	return s
}
