package components

// layouts.go renders the layout contracts: structural containers whose whole
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
	"around": style.JustifyAround,
}

// Stack renders StackProps: a vertical flex column.
func Stack(p StackProps, children ...g.Node) g.Node {
	flow := FlexLayout{Direction: style.FlexCol, Gap: gapOr(p.Gap, style.S4), Align: "normal", Justify: "normal"}
	cl := clFlex.FlexDir(flow.Direction).Gap(flow.Gap)
	if a, ok := alignItems[p.Align]; ok {
		flow.Align = a
		cl = cl.Items(flow.Align)
	}
	nodes := baseAttrs(p.ComponentProps)
	nodes = append(nodes, classes(cl.Compile(), p.Class))
	nodes = append(nodes, children...)
	return withLayout(h.Div(nodes...), p.ComponentProps, flow)
}

// Flex renders FlexProps.
func Flex(p FlexProps, children ...g.Node) g.Node {
	flow := FlexLayout{Direction: style.FlexRow, Gap: gapOr(p.Gap, style.S4), Align: "normal", Justify: "normal", Wrap: p.Wrap}
	cl := clFlex.Gap(flow.Gap)
	if p.Direction == "col" || p.Direction == "column" {
		flow.Direction = style.FlexCol
	}
	cl = cl.FlexDir(flow.Direction)
	if flow.Wrap {
		cl = cl.FlexWrap()
	}
	if a, ok := alignItems[p.Align]; ok {
		flow.Align = a
		cl = cl.Items(flow.Align)
	}
	if j, ok := justifyContent[p.Justify]; ok {
		flow.Justify = j
		cl = cl.Justify(flow.Justify)
	}
	nodes := baseAttrs(p.ComponentProps)
	nodes = append(nodes, classes(cl.Compile(), p.Class))
	nodes = append(nodes, children...)
	return withLayout(h.Div(nodes...), p.ComponentProps, flow)
}

func gridColumns(value string) int {
	cols := 1
	switch value {
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
	return cols
}

// Grid renders a base column count with optional overrides at named breakpoints.
// An omitted override inherits the preceding width. Unsupported counts use one.
func Grid(p GridProps, children ...g.Node) g.Node {
	cl := clGrid.GridCols(gridColumns(p.Columns)).Gap(gapOr(p.Gap, style.S4))
	for _, override := range []struct {
		at    style.Breakpoint
		value string
	}{{style.BreakpointSM, p.SM}, {style.BreakpointMD, p.MD}, {style.BreakpointLG, p.LG}} {
		if override.value != "" {
			cl = cl.Breakpoint(override.at, func(c style.ClassList) style.ClassList {
				return c.GridCols(gridColumns(override.value))
			})
		}
	}
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
