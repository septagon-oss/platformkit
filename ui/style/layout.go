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

// Prefix returns the Tailwind breakpoint prefix including the trailing
// colon, e.g. "sm:". Zero value returns empty string (no prefix).
func (b Breakpoint) Prefix() string {
	if b == "" {
		return ""
	}
	return string(b) + ":"
}

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
