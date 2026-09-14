package components

// layout_description.go is what a layout component says about its own root:
// the flex declarations Stack and Flex wrap their element in, so that a design
// export can describe direction, gap and alignment from source rather than by
// measuring rendered geometry. The description is data a component owns; the
// observation that reads it back lives in ui/components/examples.

import (
	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/ui/style"
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

// DeclaredLayout is the root layout a constructor declared for node, when node
// is the constructor's own result. It is the one seam the example recorder
// needs: finding a layout somewhere inside an arbitrary wrapper would not own
// its root, so a wrapped node reports none.
func DeclaredLayout(node g.Node) (LayoutDescription, bool) {
	owned, ok := node.(*layoutNode)
	if !ok {
		return LayoutDescription{}, false
	}
	return owned.layout, true
}
