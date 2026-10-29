package ui

import (
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/css"
)

// base is the small layer no utility can express: the document's own box model,
// margins, colours and type. Everything else in the stylesheet is a utility a
// component asked for, and the page's own box model, margins, colours and type.
//
// It is written here rather than as a vendored reset, because a reset is a
// thousand lines of undoing decisions browsers stopped making a decade ago.
//
// What lives here is what a utility may still overrule: every selector is an
// element or a universal selector, so a class on the element outspecifies it in
// any layer. A rule about one of the kernel's own components does not belong
// here — see componentState, which sits in the layer the utilities sit in.
func base() *css.Sheet {
	s := css.NewSheet()
	v := func(name string) css.Value { return css.VarRef(name, "") }
	// Width utilities opt into a border; CSS otherwise defaults to style none.
	// Start at zero so undecorated elements do not acquire a medium-width border.
	s.Select("*, *::before, *::after",
		css.Decl("box-sizing", css.Literal("border-box")),
		css.Decl("border-width", css.Literal("0")),
		css.Decl("border-style", css.Literal("solid")))
	s.Select("html",
		css.Decl("-webkit-text-size-adjust", css.Literal("100%")),
		css.Decl("font-family", v("pk-font-body")),
		css.Decl("line-height", css.Literal("1.5")))
	s.Select("body",
		css.Decl("margin", css.Literal("0")),
		css.Decl("min-height", css.Literal("100vh")),
		css.Decl("background-color", v("pk-color-surface-canvas")),
		css.Decl("color", v("pk-color-text-primary")),
		css.Decl("-moz-osx-font-smoothing", css.Literal("grayscale")),
		css.Decl("-webkit-font-smoothing", css.Literal("antialiased")))
	s.Select("h1, h2, h3, h4, h5, h6",
		css.Decl("font-family", v("pk-font-display")),
		css.Decl("margin", css.Literal("0")),
		css.Decl("line-height", css.Literal("1.2")))
	s.Select("p, figure, blockquote, dl, dd", css.Decl("margin", css.Literal("0")))
	s.Select("code, pre, kbd, samp", css.Decl("font-family", v("pk-font-mono")))
	s.Select("button, input, select, textarea",
		css.Decl("font", css.Literal("inherit")),
		css.Decl("color", css.Literal("inherit")))
	// A button with no background of its own gets the browser's, and the
	// browser's under `color-scheme: dark` is a mid grey nothing here was
	// designed against: the tabs' labels failed contrast in the dark theme
	// against #6b6b6b and passed in the light one against a near-white, which
	// is how a defect hides. Every button this application renders declares its
	// own surface, so the default is no surface at all.
	s.Select("button", css.Decl("background-color", css.Literal("transparent")))
	s.Select("table", css.Decl("border-collapse", css.Literal("collapse")))
	// A navigation list is not a bulleted list. The marker inherits the
	// document's text colour rather than the link's, so on the inverted sidebar
	// it was invisible in the light theme and a row of dots in the dark one —
	// which is how a defect ships: it looked right in the theme it was built in.
	s.Select("nav ul, nav ol",
		css.Decl("list-style", css.Literal("none")),
		css.Decl("margin", css.Literal("0")),
		css.Decl("padding", css.Literal("0")))
	s.Select("a", css.Decl("color", css.Literal("inherit")), css.Decl("text-decoration", css.Literal("none")))
	s.Select("img, svg", css.Decl("display", css.Literal("block")), css.Decl("max-width", css.Literal("100%")))
	s.Select("dialog::backdrop", css.Decl("background", css.Literal("rgb(0 0 0 / 0.45)")))
	s.Media("(prefers-reduced-motion: reduce)", func(inner *css.Sheet) {
		// Override ordinary utility and consumer rules while retaining completion
		// events for declared animations and transitions. A nonzero duration must
		// not activate the default transition-property: all on ordinary content;
		// later utility and consumer declarations can still name their properties.
		inner.Select("*, *::before, *::after",
			css.Decl("animation-duration", css.Literal("0.01ms !important")),
			css.Decl("animation-iteration-count", css.Literal("1 !important")),
			css.Decl("transition-property", css.Literal("none")),
			css.Decl("transition-duration", css.Literal("0.01ms !important")),
			css.Decl("scroll-behavior", css.Literal("auto !important")))
	})
	return s
}

// componentState places component-owned state rules beside their utilities.
// Sharing the components layer preserves specificity for hidden/modal/checkbox
// state; appearing first preserves utility precedence on equal selectors.
func componentState() *css.Sheet {
	s := css.NewSheet()
	s.Merge(components.ProseStyle()).Merge(components.SketchStyle())
	// The rich text field's chrome, and the published page's own prose rules
	// inside its editing surface: what an author formats is dressed the way the
	// reader will see it.
	s.Merge(richTextChrome())
	s.Merge(sketchChrome())
	s.Merge(components.ProseStyleFor(`[data-richtext-surface] [contenteditable="true"]`))
	v := func(name string) css.Value { return css.VarRef(name, "") }
	// Component layout utilities must respect Hidden. Keep the override scoped
	// so consumer HTML can reveal its own hidden content in print styles.
	s.Select("[data-component][hidden]", css.Decl("display", css.Literal("none !important")))
	s.Select("[data-component=button]", css.Decl("border-radius", v("pk-radius-button")))
	s.Select("[data-component=card]", css.Decl("border-radius", v("pk-radius-card")))
	// Text measure belongs to the components, including full-width Gallery previews.
	s.Select("[data-component=alert] p, [data-component=data-list] p, [data-component=timeline] p",
		css.Decl("max-width", css.Literal("37.5em")), css.Decl("overflow-wrap", css.Literal("anywhere")))
	s.Select("[data-modal-panel]", css.Decl("border-radius", v("pk-radius-modal")))
	// A native checkbox owns value and keyboard state. Its projected indicator
	// follows the input even without JavaScript and after native form reset.
	s.Select("[data-component=checkbox]:has(> input:enabled)",
		css.Decl("opacity", css.Literal("1")), css.Decl("cursor", css.Literal("pointer")))
	s.Select("[data-component=checkbox][hidden]", css.Decl("display", css.Literal("none !important")))
	s.Select("[data-component=checkbox] > [data-checkbox-box]",
		css.Decl("background-color", v("pk-color-surface-primary")),
		css.Decl("border-color", v("pk-color-border-default")),
		css.Decl("color", v("pk-color-accent-on")))
	s.Select("[data-component=checkbox] > input:is(:checked,:indeterminate) + [data-checkbox-box]",
		css.Decl("background-color", v("pk-color-accent-default")),
		css.Decl("border-color", v("pk-color-accent-default")))
	s.Select("[data-component=checkbox] [data-checkbox-checkmark], [data-component=checkbox] [data-checkbox-bar]",
		css.Decl("display", css.Literal("none !important")))
	s.Select("[data-component=checkbox] > input:checked:not(:indeterminate) + [data-checkbox-box] [data-checkbox-checkmark], [data-component=checkbox] > input:indeterminate + [data-checkbox-box] [data-checkbox-bar]",
		css.Decl("display", css.Literal("block !important")))
	s.Select("[data-component=checkbox] [data-checkbox-bar]", css.Decl("background-color", css.Literal("currentColor")))
	s.Select("[data-component=checkbox] > input:focus-visible + [data-checkbox-box]",
		css.Decl("outline", css.Literal("2px solid var(--pk-color-focus)")),
		css.Decl("outline-offset", css.Literal("2px")))
	s.Select("[data-component=checkbox]:has(> input:disabled)",
		css.Decl("opacity", css.Literal("0.5")), css.Decl("cursor", css.Literal("not-allowed")))
	s.Media("(forced-colors: active)", func(s *css.Sheet) {
		s.Select("[data-component=checkbox] > [data-checkbox-box]",
			css.Decl("forced-color-adjust", css.Literal("none")),
			css.Decl("background-color", css.Literal("Canvas")),
			css.Decl("border-color", css.Literal("CanvasText")),
			css.Decl("color", css.Literal("CanvasText")))
		s.Select("[data-component=checkbox] > input:focus-visible + [data-checkbox-box]",
			css.Decl("outline-color", css.Literal("Highlight")))
		s.Select("[data-component=checkbox] > input:is(:checked,:indeterminate) + [data-checkbox-box]",
			css.Decl("background-color", css.Literal("Highlight")),
			css.Decl("border-color", css.Literal("Highlight")),
			css.Decl("color", css.Literal("HighlightText")))
		s.Select("[data-component=checkbox]:has(> input:disabled)", css.Decl("opacity", css.Literal("1")))
		s.Select("[data-component=checkbox] > input:disabled + [data-checkbox-box]",
			css.Decl("border-color", css.Literal("GrayText")))
	})
	s.Select("dialog[data-component=modal]",
		css.Decl("width", css.Literal("100%")), css.Decl("height", css.Literal("100%")),
		css.Decl("max-width", css.Literal("none")), css.Decl("max-height", css.Literal("none")),
		css.Decl("margin", css.Literal("0")), css.Decl("border", css.Literal("0")),
		css.Decl("background", css.Literal("transparent")), css.Decl("color", css.Literal("inherit")))
	s.Select("dialog[data-component=modal]:not([open])", css.Decl("display", css.Literal("none")))
	s.Select("dialog[data-component=modal]::backdrop", css.Decl("background", css.Literal("transparent")))
	sharedComponentRules(s)
	guidedRules(s)
	return s
}
