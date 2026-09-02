package style

// Display is a typed CSS display mode.
type Display string

const (
	DisplayBlock       Display = "block"
	DisplayInline      Display = "inline"
	DisplayInlineBlock Display = "inline-block"
	DisplayFlex        Display = "flex"
	DisplayInlineFlex  Display = "inline-flex"
	DisplayGrid        Display = "grid"
	DisplayInlineGrid  Display = "inline-grid"
	DisplayHidden      Display = "hidden"
	// DisplayContents makes the element invisible to layout while its
	// children remain positioned as if they were the element's
	// siblings (CSS `display: contents`). Used by pass-through
	// wrappers such as the tooltip's trigger slot.
	DisplayContents Display = "contents"
	// Table display modes — used by responsive-table components to
	// swap between stacked mobile view and tabular desktop view.
	DisplayTable            Display = "table"
	DisplayInlineTable      Display = "inline-table"
	DisplayTableCaption     Display = "table-caption"
	DisplayTableCell        Display = "table-cell"
	DisplayTableColumn      Display = "table-column"
	DisplayTableColumnGroup Display = "table-column-group"
	DisplayTableFooterGroup Display = "table-footer-group"
	DisplayTableHeaderGroup Display = "table-header-group"
	DisplayTableRow         Display = "table-row"
	DisplayTableRowGroup    Display = "table-row-group"
	DisplayFlowRoot         Display = "flow-root"
	DisplayListItem         Display = "list-item"
)

// Items maps to align-items.
type Items string

const (
	ItemsStart    Items = "start"
	ItemsEnd      Items = "end"
	ItemsCenter   Items = "center"
	ItemsBaseline Items = "baseline"
	ItemsStretch  Items = "stretch"
)

// Justify maps to justify-content.
type Justify string

const (
	JustifyStart   Justify = "start"
	JustifyEnd     Justify = "end"
	JustifyCenter  Justify = "center"
	JustifyBetween Justify = "between"
	JustifyAround  Justify = "around"
	JustifyEvenly  Justify = "evenly"
)

// FlexDir is a typed flex-direction.
type FlexDir string

const (
	FlexRow        FlexDir = "row"
	FlexRowReverse FlexDir = "row-reverse"
	FlexCol        FlexDir = "col"
	FlexColReverse FlexDir = "col-reverse"
)

// Position is a typed CSS position.
type Position string

const (
	PositionStatic   Position = "static"
	PositionRelative Position = "relative"
	PositionAbsolute Position = "absolute"
	PositionFixed    Position = "fixed"
	PositionSticky   Position = "sticky"
)

// Breakpoint is a typed responsive breakpoint (Tailwind defaults).
type Breakpoint string

func (b Breakpoint) IsZero() bool { return b == "" }

// Prefix returns the Tailwind breakpoint prefix including the trailing
// colon, e.g. "sm:". Zero value returns empty string (no prefix).
func (b Breakpoint) Prefix() string {
	if b == "" {
		return ""
	}
	return string(b) + ":"
}

// String returns the breakpoint key.
func (b Breakpoint) String() string { return string(b) }

const (
	BreakpointSM  Breakpoint = "sm"  // >= 640px
	BreakpointMD  Breakpoint = "md"  // >= 768px
	BreakpointLG  Breakpoint = "lg"  // >= 1024px
	BreakpointXL  Breakpoint = "xl"  // >= 1280px
	Breakpoint2XL Breakpoint = "2xl" // >= 1536px
)

// AllDisplays returns every Display const in stable order.
func AllDisplays() []Display {
	return []Display{
		DisplayBlock, DisplayInline, DisplayInlineBlock,
		DisplayFlex, DisplayInlineFlex, DisplayGrid, DisplayInlineGrid,
		DisplayHidden,
	}
}

// AllItems returns every Items const in stable order.
func AllItems() []Items {
	return []Items{ItemsStart, ItemsEnd, ItemsCenter, ItemsBaseline, ItemsStretch}
}

// AllJustify returns every Justify const in stable order.
func AllJustify() []Justify {
	return []Justify{
		JustifyStart, JustifyEnd, JustifyCenter,
		JustifyBetween, JustifyAround, JustifyEvenly,
	}
}

// AllFlexDirs returns every FlexDir const in stable order.
func AllFlexDirs() []FlexDir {
	return []FlexDir{FlexRow, FlexRowReverse, FlexCol, FlexColReverse}
}

// AllPositions returns every Position const in stable order.
func AllPositions() []Position {
	return []Position{
		PositionStatic, PositionRelative, PositionAbsolute,
		PositionFixed, PositionSticky,
	}
}

// AllBreakpoints returns every Breakpoint const in stable order.
func AllBreakpoints() []Breakpoint {
	return []Breakpoint{
		BreakpointSM, BreakpointMD, BreakpointLG, BreakpointXL, Breakpoint2XL,
	}
}
