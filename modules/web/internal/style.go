package internal

import (
	"github.com/septagon-oss/platformkit/ui/css"
	"github.com/septagon-oss/platformkit/ui/style"
)

// The site's own class lists: a page, a bar, a brand, a navigation, a main
// column and a footer. Everything else on the page is a component.
var (
	clPage   = style.New().MinHeightScreen().Bg(style.SurfaceSecondary).TextColor(style.FgPrimary)
	clHeader = style.New().Display(style.DisplayFlex).Items(style.ItemsCenter).Justify(style.JustifyBetween).FlexWrap().Gap(style.S6).PaddingX(style.S6).PaddingY(style.S4).Bg(style.SurfacePrimary).BorderBottom(style.Border1).BorderColor(style.BorderPrimary)
	clBrand  = style.New().Display(style.DisplayFlex).Items(style.ItemsCenter).Gap(style.S3)
	clLogo   = style.New().Height(style.S8).Width(style.S8).ObjectContain()
	clTitle  = style.New().FontFamily(style.FontSerif).FontSize(style.TextXL).FontWeight(style.FontBold).Tracking(style.TrackingTight).TextColor(style.FgPrimary).NoUnderline()
	clNav    = style.New().Display(style.DisplayFlex).Items(style.ItemsCenter).FlexWrap().Gap(style.S4)
	clMain   = style.New().PaddingX(style.S6).PaddingY(style.S8)
	clFooter = style.New().PaddingX(style.S6).PaddingY(style.S8).FontSize(style.TextXS).TextColor(style.FgMuted).BorderTop(style.Border1).BorderColor(style.BorderPrimary)
)

func lists() []style.ClassList {
	return []style.ClassList{clPage, clHeader, clBrand, clLogo, clTitle, clNav, clMain, clFooter}
}

// prose styles what the Markdown renderer emits inside an article. The
// elements are the renderer's, not a component's, so they are addressed by
// element under one attribute rather than by class; the colours are the
// theme's tokens, so a tenant's palette reaches the body text too.
func prose() *css.Sheet {
	lit, ref := css.Literal, func(name string) css.Value { return css.VarRef(name, "") }
	s := css.NewSheet()
	s.Select("[data-prose]", css.Decl("line-height", lit("1.7")))
	s.Select("[data-prose] h1", css.Decl("font-size", lit("2rem")), css.Decl("line-height", lit("1.2")), css.Decl("margin", lit("0 0 1rem")))
	s.Select("[data-prose] h2", css.Decl("font-size", lit("1.5rem")), css.Decl("line-height", lit("1.3")), css.Decl("margin", lit("2rem 0 0.75rem")))
	s.Select("[data-prose] h3", css.Decl("font-size", lit("1.25rem")), css.Decl("margin", lit("1.5rem 0 0.5rem")))
	s.Select("[data-prose] p, [data-prose] ul, [data-prose] ol, [data-prose] blockquote, [data-prose] pre, [data-prose] table", css.Decl("margin", lit("0 0 1rem")))
	s.Select("[data-prose] ul, [data-prose] ol", css.Decl("padding-left", lit("1.5rem")))
	s.Select("[data-prose] ul", css.Decl("list-style", lit("disc")))
	s.Select("[data-prose] ol", css.Decl("list-style", lit("decimal")))
	s.Select("[data-prose] a", css.Decl("color", ref("pk-color-accent-default")), css.Decl("text-decoration", lit("underline")))
	s.Select("[data-prose] blockquote", css.Decl("border-left", lit("3px solid")), css.Decl("border-color", ref("pk-color-border-strong")), css.Decl("padding-left", lit("1rem")), css.Decl("color", ref("pk-color-text-muted")))
	s.Select("[data-prose] pre", css.Decl("padding", lit("1rem")), css.Decl("overflow-x", lit("auto")), css.Decl("border-radius", lit("0.5rem")), css.Decl("background", ref("pk-color-surface-muted")))
	s.Select("[data-prose] code", css.Decl("font-family", ref("pk-font-mono")), css.Decl("font-size", lit("0.925em")))
	s.Select("[data-prose] img", css.Decl("max-width", lit("100%")), css.Decl("height", lit("auto")))
	s.Select("[data-prose] table", css.Decl("border-collapse", lit("collapse")), css.Decl("width", lit("100%")))
	s.Select("[data-prose] th, [data-prose] td", css.Decl("border", lit("1px solid")), css.Decl("border-color", ref("pk-color-border-default")), css.Decl("padding", lit("0.5rem")), css.Decl("text-align", lit("left")))
	return s
}
