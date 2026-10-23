package components

import (
	"github.com/septagon-oss/platformkit/ui/style"
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// Disclosure progressively reveals a form or detail without a script or dialog.
// Native details owns keyboard activation, expanded state and focus order.
type DisclosureProps struct {
	ComponentProps
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Open        bool   `json:"open,omitzero"`
	// Flush draws no border and no inset, so the disclosure sits on its column's
	// edge: for chrome, where a box would be a third alignment on the page.
	Flush bool `json:"flush,omitzero"`
}

func Disclosure(p DisclosureProps, children ...g.Node) g.Node {
	frame, head, body := clDisclosure, clDisclosureSummary, clDisclosureBody
	if p.Flush {
		frame, head, body = clDisclosureFlush, clDisclosureFlushSummary, clDisclosureFlushBody
	}
	nodes := append(baseAttrs(p.ComponentProps), classes(frame.Compile(), p.Class), g.Attr("data-component", "disclosure"))
	if p.Open {
		nodes = append(nodes, h.Open())
	}
	summary := []g.Node{h.Class(head.Compile()), g.Text(p.Label)}
	if p.Description != "" {
		summary = append(summary, h.Span(h.Class(clDisclosureDescription.Compile()), g.Text(" · "+p.Description)))
	}
	nodes = append(nodes, h.Summary(summary...), h.Div(h.Class(body.Compile()), g.Group(children)))
	return h.Details(nodes...)
}

var (
	clDisclosure            = style.New().Border(style.Border1).BorderColor(style.BorderPrimary).Rounded(style.RadiusMD).Bg(style.SurfacePrimary)
	clDisclosureSummary     = style.New().Padding(style.S4).FontSize(style.TextSM).FontWeight(style.FontSemibold).Cursor(style.CursorPointer).Merge(clFocusRing)
	clDisclosureDescription = style.New().FontWeight(style.FontNormal).TextColor(style.FgSecondary)
	clDisclosureBody        = style.New().Padding(style.S4).BorderTop(style.Border1).BorderColor(style.BorderPrimary)

	clDisclosureFlush        = style.New()
	clDisclosureFlushSummary = style.New().PaddingY(style.S2).FontSize(style.TextSM).FontWeight(style.FontSemibold).Cursor(style.CursorPointer).Merge(clFocusRing)
	clDisclosureFlushBody    = style.New().PaddingY(style.S2)
)
