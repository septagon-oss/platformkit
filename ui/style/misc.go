package style

// BorderWidth is a typed border thickness step.
type BorderWidth string

const (
	Border0 BorderWidth = "0"
	Border1 BorderWidth = ""  // base `border` class, no suffix
	Border2 BorderWidth = "2" // border-2
	Border4 BorderWidth = "4"
	Border8 BorderWidth = "8"
)

// RingWidth is a typed focus-ring thickness step.
type RingWidth string

const (
	Ring0 RingWidth = "0"
	Ring1 RingWidth = "1"
	Ring2 RingWidth = "2"
	Ring4 RingWidth = "4"
	Ring8 RingWidth = "8"
)

// RingOffset is a typed ring-offset thickness step.
type RingOffset string

const (
	RingOffset0 RingOffset = "0"
	RingOffset1 RingOffset = "1"
	RingOffset2 RingOffset = "2"
	RingOffset4 RingOffset = "4"
	RingOffset8 RingOffset = "8"
)

// Opacity is a typed opacity percentage step.
type Opacity string

const (
	Opacity0   Opacity = "0"
	Opacity5   Opacity = "5"
	Opacity10  Opacity = "10"
	Opacity20  Opacity = "20"
	Opacity25  Opacity = "25"
	Opacity30  Opacity = "30"
	Opacity40  Opacity = "40"
	Opacity50  Opacity = "50"
	Opacity60  Opacity = "60"
	Opacity70  Opacity = "70"
	Opacity75  Opacity = "75"
	Opacity80  Opacity = "80"
	Opacity90  Opacity = "90"
	Opacity95  Opacity = "95"
	Opacity100 Opacity = "100"
)

// Cursor is a typed cursor style.
type Cursor string

const (
	CursorAuto       Cursor = "auto"
	CursorDefault    Cursor = "default"
	CursorPointer    Cursor = "pointer"
	CursorWait       Cursor = "wait"
	CursorText       Cursor = "text"
	CursorMove       Cursor = "move"
	CursorHelp       Cursor = "help"
	CursorNotAllowed Cursor = "not-allowed"
	CursorProgress   Cursor = "progress"
	CursorCrosshair  Cursor = "crosshair"
	CursorGrab       Cursor = "grab"
	CursorGrabbing   Cursor = "grabbing"
)

// PointerEvents is a typed pointer-events style.
type PointerEvents string

const (
	PointerAuto PointerEvents = "auto"
	PointerNone PointerEvents = "none"
)

// Outline is a typed outline style.
type Outline string

const (
	OutlineNone   Outline = "none"
	OutlineSolid  Outline = "solid"
	OutlineDashed Outline = "dashed"
	OutlineDotted Outline = "dotted"
	OutlineDouble Outline = "double"
)

// Select is a typed user-select style.
type Select string

const (
	SelectNone Select = "none"
	SelectText Select = "text"
	SelectAll  Select = "all"
	SelectAuto Select = "auto"
)

// ResizeMode is a typed textarea resize mode.
type ResizeMode string

const (
	ResizeModeNone ResizeMode = "none"
	ResizeModeY    ResizeMode = "y"
	ResizeModeX    ResizeMode = "x"
	ResizeModeBoth ResizeMode = "both"
)

// BorderStyle is a typed border-style utility (dashed / solid / dotted).
// The Border() method sets width + base "border"; BorderStyle() layers
// the style on top.
type BorderStyle string

const (
	BorderSolid  BorderStyle = "solid"
	BorderDashed BorderStyle = "dashed"
	BorderDotted BorderStyle = "dotted"
	BorderDouble BorderStyle = "double"
	BorderHidden BorderStyle = "hidden"
	BorderNone   BorderStyle = "none"
)

// AllBorderStyles returns every BorderStyle const in stable order.
func AllBorderStyles() []BorderStyle {
	return []BorderStyle{
		BorderSolid, BorderDashed, BorderDotted,
		BorderDouble, BorderHidden, BorderNone,
	}
}

// Overflow is a typed overflow style.
type Overflow string

const (
	OverflowAuto    Overflow = "auto"
	OverflowHidden  Overflow = "hidden"
	OverflowClip    Overflow = "clip"
	OverflowVisible Overflow = "visible"
	OverflowScroll  Overflow = "scroll"
)

// Translate is a typed translate step (for hover lifts etc.).
type Translate string

const (
	TranslateNone    Translate = "0"
	TranslatePx      Translate = "px"
	Translate0_5     Translate = "0.5"
	TranslateHalf    Translate = "1/2"
	Translate1       Translate = "1"
	TranslateNeg05   Translate = "neg-0.5" // -translate-y-0.5
	TranslateNegHalf Translate = "neg-1/2"
	TranslateNeg1    Translate = "neg-1"
	TranslateNeg2    Translate = "neg-2"
)

// PositionOffset is a non-spacing positional fraction used to anchor
// overlays. It is separate from Spacing so values such as 1/2 cannot leak
// into padding, gap, or size utilities.
type PositionOffset string

const (
	PositionHalf PositionOffset = "1/2"
)

// AllBorderWidths returns every BorderWidth in stable order.
func AllBorderWidths() []BorderWidth {
	return []BorderWidth{Border0, Border1, Border2, Border4, Border8}
}

// AllRingWidths returns every RingWidth in stable order.
func AllRingWidths() []RingWidth {
	return []RingWidth{Ring0, Ring1, Ring2, Ring4, Ring8}
}

// AllRingOffsets returns every RingOffset in stable order.
func AllRingOffsets() []RingOffset {
	return []RingOffset{RingOffset0, RingOffset1, RingOffset2, RingOffset4, RingOffset8}
}

// AllOpacities returns every Opacity in stable order.
func AllOpacities() []Opacity {
	return []Opacity{
		Opacity0, Opacity5, Opacity10, Opacity20, Opacity25, Opacity30,
		Opacity40, Opacity50, Opacity60, Opacity70, Opacity75,
		Opacity80, Opacity90, Opacity95, Opacity100,
	}
}

// AllCursors returns every Cursor in stable order.
func AllCursors() []Cursor {
	return []Cursor{
		CursorAuto, CursorDefault, CursorPointer, CursorWait,
		CursorText, CursorMove, CursorHelp, CursorNotAllowed,
		CursorProgress, CursorCrosshair, CursorGrab, CursorGrabbing,
	}
}

// AllPointerEvents returns every PointerEvents in stable order.
func AllPointerEvents() []PointerEvents {
	return []PointerEvents{PointerAuto, PointerNone}
}

// AllOutlines returns every Outline in stable order.
func AllOutlines() []Outline {
	return []Outline{
		OutlineNone, OutlineSolid, OutlineDashed,
		OutlineDotted, OutlineDouble,
	}
}

// AllSelects returns every Select in stable order.
func AllSelects() []Select {
	return []Select{SelectNone, SelectText, SelectAll, SelectAuto}
}

// AllOverflows returns every Overflow in stable order.
func AllOverflows() []Overflow {
	return []Overflow{
		OverflowAuto, OverflowHidden, OverflowClip,
		OverflowVisible, OverflowScroll,
	}
}

// AllTranslates returns every Translate in stable order.
func AllTranslates() []Translate {
	return []Translate{
		TranslateNone, TranslatePx, Translate0_5, TranslateHalf, Translate1,
		TranslateNeg05, TranslateNegHalf, TranslateNeg1, TranslateNeg2,
	}
}

// AllPositionOffsets returns every governed overlay-position fraction.
func AllPositionOffsets() []PositionOffset {
	return []PositionOffset{PositionHalf}
}
