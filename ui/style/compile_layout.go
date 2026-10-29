package style

// classDisplay maps Display → display class.
func classDisplay(d Display) string {
	if d == "" {
		return ""
	}
	return string(d)
}

// classItems maps Items → "items-*".
func classItems(i Items) string {
	if i == "" {
		return ""
	}
	return "items-" + string(i)
}

// classJustify maps Justify → "justify-*".
func classJustify(j Justify) string {
	if j == "" {
		return ""
	}
	return "justify-" + string(j)
}

// classFlexDir maps FlexDir → flex direction.
func classFlexDir(f FlexDir) string {
	if f == "" {
		return ""
	}
	return "flex-" + string(f)
}

// classPosition maps Position → position class.
func classPosition(p Position) string {
	if p == "" {
		return ""
	}
	return string(p)
}

// classOverflow maps Overflow → "overflow-*".
func classOverflow(o Overflow) string {
	if o == "" {
		return ""
	}
	return "overflow-" + string(o)
}

// classOverflowX / classOverflowY emit axis-specific overflow.
func classOverflowX(o Overflow) string {
	if o == "" {
		return ""
	}
	return "overflow-x-" + string(o)
}

func classOverflowY(o Overflow) string {
	if o == "" {
		return ""
	}
	return "overflow-y-" + string(o)
}

// classGridCols emits "grid-cols-N" for a CSS grid template. N must be
// positive; 0 or negative → empty (the typed tables and All* enumerators flags it).
func classGridCols(n int) string {
	return classGridTracks("cols", n)
}

func classGridTracks(axis string, n int) string {
	if n <= 0 {
		return ""
	}
	return "grid-" + axis + "-" + itoa(n)
}

// classColSpan emits "col-span-N" for a grid column span.
func classColSpan(n int) string {
	if n <= 0 {
		return ""
	}
	return "col-span-" + itoa(n)
}
