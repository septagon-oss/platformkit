package ui

import "github.com/septagon-oss/platformkit/ui/css"

func guidedRules(s *css.Sheet) {
	s.Select("[data-guided]", css.Decl("max-width", css.Literal("72rem")), css.Decl("margin-inline", css.Literal("auto")))
	s.Select("[data-guided] p", css.Decl("max-inline-size", css.Literal("65ch")), css.Decl("white-space", css.Literal("pre-line")), css.Decl("overflow-wrap", css.Literal("anywhere")))
	s.Select("[data-guided] fieldset", css.Decl("min-width", css.Literal("0")))
	s.Select("[data-guided] img", css.Decl("max-height", css.Literal("24rem")), css.Decl("object-fit", css.Literal("contain")))
	s.Select("[data-guided-target], [data-guided-rest]", css.Decl("font-variant-numeric", css.Literal("tabular-nums")))
	s.Select("[data-stepper-segmented]", css.Decl("display", css.Literal("flex")), css.Decl("gap", css.Literal("var(--pk-space-2, 0.5rem)")), css.Decl("overflow-x", css.Literal("auto")), css.Decl("list-style", css.Literal("none")), css.Decl("padding", css.Literal("0")))
	s.Select("[data-stepper-segmented] > li", css.Decl("flex", css.Literal("1 0 5rem")), css.Decl("border-top", css.Literal("0.25rem solid var(--pk-color-border-default)")))
	s.Select("[data-stepper-segmented] > [aria-current=step], [data-stepper-segmented] > [data-step-state=complete]", css.Decl("border-color", css.Literal("var(--pk-color-accent-default)")))
	s.Select("[data-stepper-segmented] a", css.Decl("display", css.Literal("block")), css.Decl("white-space", css.Literal("normal")), css.Decl("min-height", css.Literal("44px")))
	s.Select("[data-guided-timers] button, [data-guided-timers] a", css.Decl("min-height", css.Literal("44px")), css.Decl("padding", css.Literal("0.5rem")))
}
