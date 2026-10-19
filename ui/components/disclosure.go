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
}

func Disclosure(p DisclosureProps, children ...g.Node) g.Node {
	nodes := append(baseAttrs(p.ComponentProps), classes(clDisclosure.Compile(), p.Class), g.Attr("data-component", "disclosure"))
	if p.Open {
		nodes = append(nodes, h.Open())
	}
	summary := []g.Node{h.Class(clDisclosureSummary.Compile()), g.Text(p.Label)}
	if p.Description != "" {
		summary = append(summary, h.Span(h.Class(clDisclosureDescription.Compile()), g.Text(" · "+p.Description)))
	}
	nodes = append(nodes, h.Summary(summary...), h.Div(h.Class(clDisclosureBody.Compile()), g.Group(children)))
	return h.Details(nodes...)
}

var (
	clDisclosure            = style.New().Border(style.Border1).BorderColor(style.BorderPrimary).Rounded(style.RadiusMD).Bg(style.SurfacePrimary)
	clDisclosureSummary     = style.New().Padding(style.S4).FontSize(style.TextSM).FontWeight(style.FontSemibold).Cursor(style.CursorPointer).Merge(clFocusRing)
	clDisclosureDescription = style.New().FontWeight(style.FontNormal).TextColor(style.FgSecondary)
	clDisclosureBody        = style.New().Padding(style.S4).BorderTop(style.Border1).BorderColor(style.BorderPrimary)
)
