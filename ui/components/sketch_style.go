package components

import (
	"github.com/septagon-oss/platformkit/ui/css"
)

// SketchStyle carries the component's own geometry: a drawing is one SVG that
// must fill the box it is painted in at the square its strokes are stored
// against, so every reader and preview scales identically without copying
// geometry into product markup.
//
// Only `[data-component="sketch"]` is addressed here. The board a person draws
// into, the controls under it and the size a screen reads a saved drawing at are
// markup ui/forms and ui/resource render, and ui/sketch_style.go carries their
// rules: this package exports the vocabulary its own markup emits, and styling a
// hook it never renders would claim it.
func SketchStyle() *css.Sheet {
	v := func(name string) css.Value { return css.VarRef(name, "") }
	lit := css.Literal
	s := css.NewSheet()
	s.Select(`[data-component="sketch"] svg`,
		css.Decl("display", lit("block")),
		css.Decl("width", lit("100%")),
		css.Decl("aspect-ratio", lit("1")),
		css.Decl("overflow", lit("hidden")))
	s.Select(`[data-component="sketch"]`,
		css.Decl("color", v("pk-color-text-primary")),
		css.Decl("min-width", lit("0")))
	return s
}
