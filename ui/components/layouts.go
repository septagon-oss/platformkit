package components

// go renders the layout contracts: structural containers whose whole
// job is arranging children. They accept children as trailing gomponents
// nodes because layout without content is meaningless.

import (
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/ui/style"
)

func gapOr(gap string, fallback style.Spacing) style.Spacing {
	if s, ok := clGapScale[gap]; ok {
		return s
	}
	return fallback
}

var alignItems = map[string]style.Items{
	"start": style.ItemsStart, "center": style.ItemsCenter,
	"end": style.ItemsEnd, "stretch": style.ItemsStretch,
}

var justifyContent = map[string]style.Justify{
	"start": style.JustifyStart, "center": style.JustifyCenter,
	"end": style.JustifyEnd, "between": style.JustifyBetween,
}

// Stack renders StackProps: a vertical flex column.
func Stack(p StackProps, children ...g.Node) g.Node {
	cl := clStack.Gap(gapOr(p.Gap, style.S4))
	if a, ok := alignItems[p.Align]; ok {
		cl = cl.Items(a)
	}
	nodes := baseAttrs(p.ComponentProps)
	nodes = append(nodes, classes(cl.Compile(), p.Class))
	nodes = append(nodes, children...)
	return h.Div(nodes...)
}

// Flex renders FlexProps.
func Flex(p FlexProps, children ...g.Node) g.Node {
	cl := clFlex.Gap(gapOr(p.Gap, style.S4))
	if p.Direction == "col" || p.Direction == "column" {
		cl = cl.FlexDir(style.FlexCol)
	} else {
		cl = cl.FlexDir(style.FlexRow)
	}
	if p.Wrap {
		cl = cl.FlexWrap()
	}
	if a, ok := alignItems[p.Align]; ok {
		cl = cl.Items(a)
	}
	if j, ok := justifyContent[p.Justify]; ok {
		cl = cl.Justify(j)
	}
	nodes := baseAttrs(p.ComponentProps)
	nodes = append(nodes, classes(cl.Compile(), p.Class))
	nodes = append(nodes, children...)
	return h.Div(nodes...)
}

// Grid renders GridProps; Columns outside 1..12 fall back to 1.
func Grid(p GridProps, children ...g.Node) g.Node {
	cols := 1
	switch p.Columns {
	case "2":
		cols = 2
	case "3":
		cols = 3
	case "4":
		cols = 4
	case "6":
		cols = 6
	case "12":
		cols = 12
	}
	cl := clGrid.GridCols(cols).Gap(gapOr(p.Gap, style.S4))
	nodes := baseAttrs(p.ComponentProps)
	nodes = append(nodes, classes(cl.Compile(), p.Class))
	nodes = append(nodes, children...)
	return h.Div(nodes...)
}

var containerWidths = map[string]style.MaxWidth{
	"sm": style.MaxWSM, "md": style.MaxWMD, "lg": style.MaxWLG, "xl": style.MaxWXL,
	"2xl": style.MaxW2XL, "4xl": style.MaxW4XL, "7xl": style.MaxW7XL, "full": style.MaxWFull,
}

// Container renders ContainerProps: a centered max-width column.
func Container(p ContainerProps, children ...g.Node) g.Node {
	w := style.MaxW7XL
	if mw, ok := containerWidths[p.MaxWidth]; ok {
		w = mw
	}
	cl := clContainer.MaxWScaled(w)
	nodes := baseAttrs(p.ComponentProps)
	nodes = append(nodes, classes(cl.Compile(), p.Class))
	nodes = append(nodes, children...)
	return h.Div(nodes...)
}
