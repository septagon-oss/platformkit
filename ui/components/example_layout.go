package components

import (
	"github.com/septagon-oss/platformkit/ui/style"
	g "maragu.dev/gomponents"
)

// LayoutDescription describes root declarations, not used geometry or native
// support. Unknown has a reason and no Flex; flex has Flex and no reason.
type LayoutDescription struct {
	Kind   string      `json:"kind"`
	Reason string      `json:"reason,omitempty"`
	Flex   *FlexLayout `json:"flex,omitempty"`
}

// FlexLayout is the source-flex-declarations.v1 feature. Gap names the existing
// style spacing step. Normal means CSS initial alignment, not an inferred value.
// Sizing, writing mode, typography and descendant placement are not described.
type FlexLayout struct {
	Direction style.FlexDir `json:"direction"`
	Gap       style.Spacing `json:"gap"`
	Align     style.Items   `json:"align"`
	Justify   style.Justify `json:"justify"`
	Wrap      bool          `json:"wrap"`
}

// The private wrapper preserves NodeFunc's rendering, String and element type.
// Only the actual constructor result can supply the enclosing capture's layout;
// finding a layout somewhere inside an arbitrary wrapper does not own its root.
type layoutNode struct {
	g.NodeFunc
	layout LayoutDescription
}

func withLayout(node g.Node, props ComponentProps, flow FlexLayout) g.Node {
	layout := LayoutDescription{Kind: "flex", Flex: new(flow)}
	if props.Class != "" || len(props.Attrs) != 0 || props.Hidden {
		layout = LayoutDescription{Kind: "unknown", Reason: "root escape hatch or hidden state"}
	}
	return &layoutNode{NodeFunc: g.NodeFunc(node.Render), layout: layout}
}

// DescribeWithLayout opts into source declarations without changing Describe's
// v1 serialization. Unknown ancestors/children still require explicit refusal.
func (e Example) DescribeWithLayout() (ExampleDescription, error) {
	return e.describe(true)
}
