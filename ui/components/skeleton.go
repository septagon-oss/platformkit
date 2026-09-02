package components

// skeleton.go renders the loading state of the library. A skeleton is the
// loading rendering of a component, not a separate component: primitives hold
// the geometry of pending content, twins mirror Table and Card exactly (same
// class lists), and DeferredSlot is the HTMX seam that swaps the finished
// fragment in.
//
// Accessibility contract: each placeholder is aria-hidden (it carries no
// information), while the enclosing DeferredSlot announces the pending state
// once via aria-busy. Motion is AnimatePulse — a decorative opacity fade whose
// pk-pulse keyframes tw/emission emits in Base().

import (
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/ui/style"
)

// Skeleton renders SkeletonProps. Shapes: "block" (full-width rectangle,
// the default), "text" (one or more prose lines, last line short), "circle"
// (avatar-sized disc). Unknown shape or size strings fall back to the
// defaults, matching the contracts' "data schema, not behavior" stance.
func Skeleton(p SkeletonProps) g.Node {
	size := p.Size
	if size == "" {
		size = "md"
	}
	switch p.Shape {
	case "circle":
		cl := clSkeleton.Merge(variantOr(clSkeletonCircleSize, size, "md"))
		return h.Div(append(baseAttrs(p.ComponentProps),
			classes(cl.Compile(), p.Class), g.Attr("aria-hidden", "true"))...)
	case "text":
		lines := max(p.Lines, 1)
		if lines == 1 {
			cl := skeletonLine(size, false)
			return h.Div(append(baseAttrs(p.ComponentProps),
				classes(cl.Compile(), p.Class), g.Attr("aria-hidden", "true"))...)
		}
		nodes := append(baseAttrs(p.ComponentProps),
			classes(clSkeletonText.Compile(), p.Class), g.Attr("aria-hidden", "true"))
		for i := 0; i < lines; i++ {
			nodes = append(nodes, h.Div(h.Class(skeletonLine(size, i == lines-1).Compile())))
		}
		return h.Div(nodes...)
	default:
		cl := clSkeleton.Merge(variantOr(clSkeletonBlockSize, size, "md"))
		return h.Div(append(baseAttrs(p.ComponentProps),
			classes(cl.Compile(), p.Class), g.Attr("aria-hidden", "true"))...)
	}
}

// skeletonLine is one pulsing prose line; the last line of a multi-line text
// placeholder stops short so the paragraph reads as prose, not a slab.
func skeletonLine(size string, last bool) style.ClassList {
	width := clSkeletonLine
	if last {
		width = clSkeletonLineLast
	}
	return clSkeleton.Merge(width).Merge(variantOr(clSkeletonLineSize, size, "md"))
}

// TableSkeleton renders TableSkeletonProps: the loading rendering of
// Table, built from Table's own class lists so the swap-in causes no layout
// shift. Defaults: 4 columns, 3 rows.
func TableSkeleton(p TableSkeletonProps) g.Node {
	cols := p.Columns
	if cols < 1 {
		cols = 4
	}
	rows := p.Rows
	if rows < 1 {
		rows = 3
	}
	tdClass := clTableTd
	if p.Compact {
		tdClass = clTableTdC
	}
	cellLine := skeletonLine("sm", false)

	headCells := make([]g.Node, 0, cols)
	for i := 0; i < cols; i++ {
		headCells = append(headCells, h.Th(
			h.Class(clTableTh.Compile()), g.Attr("scope", "col"),
			h.Div(h.Class(cellLine.Compile())),
		))
	}

	bodyRows := make([]g.Node, 0, rows)
	for i := 0; i < rows; i++ {
		cells := []g.Node{h.Class(clTableRow.Compile())}
		for j := 0; j < cols; j++ {
			cells = append(cells, h.Td(h.Class(tdClass.Compile()),
				h.Div(h.Class(cellLine.Compile()))))
		}
		bodyRows = append(bodyRows, h.Tr(cells...))
	}

	nodes := baseAttrs(p.ComponentProps)
	nodes = append(nodes, classes(clTableWrap.Compile(), p.Class),
		g.Attr("aria-hidden", "true"),
		h.Table(
			h.Class(clTable.Compile()),
			h.THead(h.Class(clTableHead.Compile()), h.Tr(headCells...)),
			h.TBody(bodyRows...),
		),
	)
	return h.Div(nodes...)
}
