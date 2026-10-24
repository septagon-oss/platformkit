package components

import (
	"github.com/septagon-oss/platformkit/ui/css"
	"github.com/septagon-oss/platformkit/ui/style"
)

// SketchStyle supplies the shared drawing geometry, tokens and focus treatment.
func SketchStyle() *css.Sheet {
	s := css.NewSheet()
	lit, token := css.Literal, func(n string) css.Value { return css.VarRef(n, "") }
	s.Select(`[data-component="sketch"] svg, [data-sketch] canvas`, css.Decl("display", lit("block")), css.Decl("width", lit("100%")), css.Decl("aspect-ratio", lit("1")), css.Decl("overflow", lit("hidden")))
	s.Select(`[data-component="sketch"], [data-sketch]`, css.Decl("color", token("pk-color-text-primary")), css.Decl("min-width", lit("0")))
	s.Select(`[data-sketch]`, css.Decl("border", lit("0")), css.Decl("padding", lit("0")), css.Decl("margin", lit("0")))
	s.Select(`[data-sketch] canvas`, css.Decl("touch-action", lit("none")), css.Decl("background", token("pk-color-surface-primary")), css.Decl("border-radius", lit(style.RadiusLG.Value())), css.Decl("outline-offset", lit(style.S1.Value())))
	s.Select(`[data-sketch] canvas:focus-visible`, css.Decl("outline", lit("2px solid currentColor")))
	s.Select(`[data-sketch] [hidden]`, css.Decl("display", lit("none !important")))
	s.Select(`[data-sketch-actions]`, css.Decl("display", lit("flex")), css.Decl("gap", lit(style.S2.Value())), css.Decl("flex-wrap", lit("wrap")), css.Decl("margin-top", lit(style.S2.Value())))
	s.Select(`[data-sketch-actions] button`, css.Decl("min-height", lit(style.S12.Value())))
	s.Select(`[data-sketch] p`, css.Decl("font-size", lit(style.TextSM.Value())), css.Decl("margin-block", lit(style.S2.Value())))
	s.Select(`[data-sketch-size="thumbnail"]`, css.Decl("width", lit(style.S16.Value())))
	s.Select(`[data-sketch-size="full"]`, css.Decl("max-width", lit(style.SFull.Value())))
	return s
}
