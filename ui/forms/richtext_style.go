package forms

import (
	"github.com/septagon-oss/platformkit/ui/css"
	"github.com/septagon-oss/platformkit/ui/style"
)

// RichTextChrome is the drawn field's chrome: one bordered editing surface, the
// rows of controls around it, and the one line the browser writes beneath it.
//
// Two rules of the page decide its shape. The surface is dressed in the prose
// rules the published page renders under — merged beside this one in ui/ui.go —
// so an author sees the thing they are writing rather than an unstyled box. And
// the whole field sits on one body step: the counter, the status line and the
// document itself are the size the field's own label is, because a form whose
// labels are one size and whose body text another is a page with two opinions
// about itself.
//
// Every rule that would set display names `:not([hidden])`. The chrome is drawn
// hidden and the browser reveals it, so an unguarded display declaration would
// paint a toolbar with no editor behind it on every page with no JavaScript.
func RichTextChrome() *css.Sheet {
	v := func(name string) css.Value { return css.VarRef(name, "") }
	lit := css.Literal
	step := func(s style.Spacing) css.Value { return lit(s.Value()) }
	around := func(above, under style.Spacing) css.Value { return lit(above.Value() + " 0 " + under.Value()) }
	size := func(f style.FontSize) css.Value { return lit(f.Value()) }
	edge := func(w style.BorderWidth) css.Value { return lit(w.Value() + " solid") }
	s := css.NewSheet()
	// Chrome that is hidden must be gone, not merely unseen: the textarea keeps a
	// `display` class of its own, which outranks the user agent's [hidden] rule,
	// and a field with two visible surfaces is the defect this chrome ended.
	s.Select("[data-richtext] [hidden]", css.Decl("display", lit("none !important")))
	s.Select("[data-richtext]", css.Decl("font-size", size(style.TextSM)))
	// The writing surface is the page's own prose column: ruled above and below,
	// kept to a measure, and with its text on the column the rest of the page keeps
	// — inline padding would indent an author's words away from the label over them
	// and put a second content edge on the page.
	s.Select("[data-richtext-surface]:not([hidden])",
		css.Decl("display", lit("block")),
		css.Decl("max-width", lit("36em")),
		css.Decl("min-height", lit("12rem")),
		css.Decl("padding", around(style.S4, style.S4)),
		css.Decl("border-width", lit("1px 0")),
		css.Decl("border-style", lit("solid")),
		css.Decl("border-color", v("pk-color-border-default")),
		css.Decl("background-color", v("pk-color-surface-primary")),
		css.Decl("overflow-wrap", lit("anywhere")))
	s.Select("[data-richtext-surface]:focus-within",
		css.Decl("border-color", v("pk-color-accent-default")))
	rows := `[data-richtext-toolbar]:not([hidden]), [data-richtext-more-panel]:not([hidden])`
	s.Select(rows,
		css.Decl("display", lit("flex")),
		css.Decl("flex-wrap", lit("wrap")),
		css.Decl("align-items", lit("center")),
		css.Decl("gap", step(style.S1)))
	// The floating pieces are the same panel three times over: it must read as a
	// thing set on top of the page, not as markup that arrived late.
	panel := `[data-richtext-bubble]:not([hidden]), [data-richtext-link]:not([hidden]), [data-richtext-menu]:not([hidden])`
	s.Select(panel,
		css.Decl("padding", step(style.S2)),
		css.Decl("border", edge(style.Border1)),
		css.Decl("border-color", v("pk-color-border-default")),
		css.Decl("border-radius", lit(style.RadiusLG.Value())),
		css.Decl("background-color", v("pk-color-surface-primary")))
	s.Select(`[data-richtext-bubble]:not([hidden])`,
		css.Decl("display", lit("flex")), css.Decl("flex-wrap", lit("wrap")), css.Decl("gap", step(style.S1)))
	s.Select(`[data-richtext-link]:not([hidden])`,
		css.Decl("display", lit("flex")), css.Decl("flex-wrap", lit("wrap")),
		css.Decl("align-items", lit("end")), css.Decl("gap", step(style.S2)))
	s.Select(`[data-richtext-menu]:not([hidden])`,
		css.Decl("display", lit("flex")), css.Decl("flex-direction", lit("column")),
		css.Decl("gap", step(style.S1)), css.Decl("max-height", lit("18rem")), css.Decl("overflow-y", lit("auto")))
	// A menu row is two lines: the block's name and one line about it.
	s.Select(`[data-richtext-menu] [data-richtext-command]`,
		css.Decl("display", lit("flex")), css.Decl("flex-direction", lit("column")),
		css.Decl("gap", step(style.S1)), css.Decl("align-items", lit("flex-start")),
		css.Decl("padding", step(style.S2)),
		css.Decl("border-radius", lit(style.RadiusMD.Value())),
		css.Decl("text-align", lit("left")),
		css.Decl("background-color", lit("transparent")),
		css.Decl("color", v("pk-color-text-primary")))
	s.Select(`[data-richtext-menu] [data-richtext-command][aria-selected="true"]`,
		css.Decl("background-color", v("pk-color-surface-muted")))
	// The draft bar and the offer slot are the field answering: one line and the
	// choices that go with it, in the place the page already laid out.
	s.Select(`[data-richtext-draft-bar]:not([hidden]), [data-richtext-offer]:not([hidden])`,
		css.Decl("display", lit("flex")), css.Decl("flex-wrap", lit("wrap")),
		css.Decl("align-items", lit("center")), css.Decl("gap", step(style.S2)),
		css.Decl("padding", step(style.S3)),
		css.Decl("border", edge(style.Border1)),
		css.Decl("border-color", v("pk-color-border-strong")),
		css.Decl("border-radius", lit(style.RadiusLG.Value())),
		css.Decl("background-color", v("pk-color-surface-muted")))
	// The counter and the status line are the field's own two sentences: one body
	// step, the column the field is, and muted because they are said about the
	// document rather than written in it.
	s.Select("[data-richtext-count], [data-richtext-status]",
		css.Decl("max-width", lit("36em")),
		css.Decl("margin", lit("0")),
		css.Decl("color", v("pk-color-text-muted")),
		css.Decl("overflow-wrap", lit("anywhere")))
	return s
}
