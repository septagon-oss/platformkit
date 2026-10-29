package ui

import "github.com/septagon-oss/platformkit/ui/css"

func choiceRules(s *css.Sheet) {
	rule := func(selector string, pairs ...string) {
		declarations := make([]css.Declaration, 0, len(pairs)/2)
		for i := 0; i < len(pairs); i += 2 {
			declarations = append(declarations, css.Decl(pairs[i], css.Literal(pairs[i+1])))
		}
		s.Select(selector, declarations...)
	}
	rule("[data-choice-surface]", "min-width", "0", "border", "0", "padding", "0", "margin-block", "1rem", "color", "var(--pk-color-text-primary)")
	rule("[data-choice-surface] legend", "font-weight", "600", "margin-block-end", "0.5rem")
	rule("[data-choice-surface] p", "max-width", "37.5em", "font-size", "inherit", "overflow-wrap", "anywhere")
	rule("[data-choice-options]", "max-height", "24rem", "overflow", "auto", "padding", "0.5rem")
	rule("[data-choice-options] [data-choice-item]", "position", "relative", "display", "inline-flex", "align-items", "center", "gap", "0.5rem", "min-height", "44px", "max-width", "100%", "padding", "0.5rem 1rem", "border", "1px solid var(--pk-color-border-default)", "border-radius", "var(--pk-radius-button)", "cursor", "pointer", "overflow-wrap", "anywhere")
	rule("[data-choice-options] [data-choice-item][hidden]", "display", "none")
	rule("[data-choice-options] [data-choice-item]:has(:checked)", "box-shadow", "inset 0 0 0 2px var(--pk-color-accent-default)")
	rule("[data-choice-item]:has(:focus-visible)", "outline", "2px solid var(--pk-color-focus)", "outline-offset", "3px")
	rule("[data-choice-options] input", "accent-color", "var(--pk-color-accent-default)")
	rule("[data-choice-check]", "visibility", "hidden")
	rule("[data-choice-item]:has(:checked) [data-choice-check]", "visibility", "visible")
	rule("[data-choice-colour]", "display", "inline-flex", "width", "2.5rem", "height", "2.5rem", "flex-shrink", "0")
	rule("[data-choice-colour] svg", "width", "100%", "height", "100%")
	rule("[data-choice-surface=tiles] [data-choice-options] > div", "display", "grid", "grid-template-columns", "repeat(auto-fit,minmax(min(100%,8rem),1fr))")
	rule("[data-choice-surface=tiles] [data-choice-item]", "flex-direction", "column", "border-radius", "var(--pk-radius-card)")
	rule("[data-choice-surface=tiles] [data-choice-item] img", "width", "100%", "height", "6rem", "object-fit", "contain")
	rule("[data-choice-summary]", "display", "flex", "align-items", "center", "flex-wrap", "wrap", "gap", "0.5rem", "padding-block", "0.75rem", "background", "var(--pk-color-surface-primary)")
	rule("[data-choice-stack]", "display", "flex", "padding-inline-start", "0.5rem")
	rule("[data-choice-stack] > *", "width", "2rem", "height", "2rem", "margin-inline-start", "-0.5rem", "border-radius", "var(--pk-radius-button)", "border", "2px solid var(--pk-color-surface-primary)", "object-fit", "cover")
	rule("[data-choice-names]", "overflow-wrap", "anywhere", "flex-basis", "100%")
	rule("[data-choice-figure] svg", "width", "100%", "height", "auto", "max-height", "24rem")
	rule("[data-choice-figure] [data-selected=true], [data-choice-figure] [data-selected=true] *", "stroke", "var(--pk-color-accent-default)", "stroke-width", "3", "stroke-dasharray", "4 2", "vector-effect", "non-scaling-stroke")
	rule("[data-choice-layout]", "display", "grid", "grid-template-columns", "repeat(auto-fit,minmax(min(100%,15rem),1fr))", "gap", "1rem")
	rule("[data-choice-read] > div", "display", "flex", "flex-wrap", "wrap", "gap", "0.5rem")
	rule("[data-choice-read-item]", "display", "inline-flex", "align-items", "center", "gap", "0.5rem", "padding", "0.5rem", "border", "1px solid var(--pk-color-border-default)", "border-radius", "var(--pk-radius-button)")
	rule("[data-choice-read-item] img", "width", "4rem", "height", "4rem", "object-fit", "contain")
	s.Media("(forced-colors: active)", func(s *css.Sheet) {
		s.Select("[data-choice-item]:has(:checked)", css.Decl("border", css.Literal("3px solid Highlight")))
		s.Select("[data-choice-item]:has(:focus-visible)", css.Decl("outline-color", css.Literal("CanvasText")))
		s.Select("[data-choice-figure] [data-selected=true], [data-choice-figure] [data-selected=true] *", css.Decl("stroke", css.Literal("Highlight")))
	})
}
