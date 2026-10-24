package ui

import (
	"github.com/septagon-oss/platformkit/ui/css"
	"github.com/septagon-oss/platformkit/ui/style"
)

// sketchChrome is the drawing field's chrome: the board a person draws into, the
// controls under it, and the size a generated screen reads a saved drawing at.
//
// It sits here rather than in ui/components for the same reason the rich text
// field's chrome does: this markup is not the component's. The board and its
// actions are drawn by ui/forms and the thumbnail/full size is chosen by
// ui/resource, so a rule that addressed them from ui/components would make that
// package answer for markup it never renders — the vocabulary ui/components
// exports is its own markup and nothing else.
//
// The board is a canvas, so it is sized by the box it is painted in: the width
// comes from the layout, the height follows the square the strokes are stored
// against, and touch is the one gesture that must not scroll the page.
func sketchChrome() *css.Sheet {
	v := func(name string) css.Value { return css.VarRef(name, "") }
	lit := css.Literal
	s := css.NewSheet()
	s.Select("[data-sketch] canvas",
		css.Decl("display", lit("block")),
		css.Decl("width", lit("100%")),
		css.Decl("aspect-ratio", lit("1")),
		css.Decl("overflow", lit("hidden")))
	s.Select("[data-sketch]",
		css.Decl("color", v("pk-color-text-primary")),
		css.Decl("min-width", lit("0")),
		css.Decl("border", lit("0")),
		css.Decl("padding", lit("0")),
		css.Decl("margin", lit("0")))
	s.Select("[data-sketch] canvas",
		css.Decl("touch-action", lit("none")),
		css.Decl("background-color", v("pk-color-surface-primary")),
		css.Decl("border-radius", lit(style.RadiusLG.Value())),
		css.Decl("outline-offset", lit(style.S1.Value())))
	s.Select("[data-sketch] canvas:focus-visible", css.Decl("outline", lit("2px solid currentColor")))
	// The actions row and the no-JavaScript note are both drawn hidden: the
	// browser shows the row and hides the note. A display rule written without
	// [hidden] would leave the fallback sentence on the page once the board works.
	s.Select("[data-sketch] [hidden]", css.Decl("display", lit("none !important")))
	s.Select("[data-sketch] p",
		css.Decl("font-size", lit(style.TextSM.Value())),
		css.Decl("margin-block", lit(style.S2.Value())))
	s.Select("[data-sketch-actions]",
		css.Decl("display", lit("flex")),
		css.Decl("gap", lit(style.S2.Value())),
		css.Decl("flex-wrap", lit("wrap")),
		css.Decl("margin-top", lit(style.S2.Value())))
	s.Select("[data-sketch-actions] button", css.Decl("min-height", lit(style.S12.Value())))
	// A list row reads a drawing at a swatch; the record reads it at the width a
	// person can still make a line out of. Both are the same SVG.
	s.Select(`[data-sketch-size="thumbnail"]`, css.Decl("width", lit(style.S16.Value())))
	s.Select(`[data-sketch-size="full"]`, css.Decl("max-width", lit(style.SFull.Value())))
	return s
}
