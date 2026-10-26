package style

// classPaddingX maps Spacing → "px-*".
func classPaddingX(s Spacing) string {
	if s == "" {
		return ""
	}
	return "px-" + string(s)
}

// classPaddingY maps Spacing → "py-*".
func classPaddingY(s Spacing) string {
	if s == "" {
		return ""
	}
	return "py-" + string(s)
}

// classPadding maps Spacing → "p-*".
func classPadding(s Spacing) string {
	if s == "" {
		return ""
	}
	return "p-" + string(s)
}

// classMarginX, classMarginY, classMargin, and per-side variants.
func classMarginX(s Spacing) string {
	if s == "" {
		return ""
	}
	return "mx-" + string(s)
}

func classMarginY(s Spacing) string {
	if s == "" {
		return ""
	}
	return "my-" + string(s)
}

func classMargin(s Spacing) string {
	if s == "" {
		return ""
	}
	return "m-" + string(s)
}

func classMarginLeft(s Spacing) string {
	if s == "" {
		return ""
	}
	return "ml-" + string(s)
}

func classMarginRight(s Spacing) string {
	if s == "" {
		return ""
	}
	return "mr-" + string(s)
}

func classMarginTop(s Spacing) string {
	if s == "" {
		return ""
	}
	return "mt-" + string(s)
}

func classMarginBottom(s Spacing) string {
	if s == "" {
		return ""
	}
	return "mb-" + string(s)
}

func classPaddingLeft(s Spacing) string {
	if s == "" {
		return ""
	}
	return "pl-" + string(s)
}

func classPaddingRight(s Spacing) string {
	if s == "" {
		return ""
	}
	return "pr-" + string(s)
}

func classPaddingTop(s Spacing) string {
	if s == "" {
		return ""
	}
	return "pt-" + string(s)
}

func classPaddingBottom(s Spacing) string {
	if s == "" {
		return ""
	}
	return "pb-" + string(s)
}

// Absolute-position offset helpers. Used when positioning absolute
// children (icons, badges, close buttons) inside a relative parent.
func classTop(s Spacing) string {
	if s == "" {
		return ""
	}
	return "top-" + string(s)
}

func classBottom(s Spacing) string {
	if s == "" {
		return ""
	}
	return "bottom-" + string(s)
}

func classLeft(s Spacing) string {
	if s == "" {
		return ""
	}
	return "left-" + string(s)
}

func classRight(s Spacing) string {
	if s == "" {
		return ""
	}
	return "right-" + string(s)
}

func classInset(s Spacing) string {
	if s == "" {
		return ""
	}
	return "inset-" + string(s)
}

func classInsetX(s Spacing) string {
	if s == "" {
		return ""
	}
	return "inset-x-" + string(s)
}

func classInsetY(s Spacing) string {
	if s == "" {
		return ""
	}
	return "inset-y-" + string(s)
}

// classGap maps Spacing → "gap-*".
func classGap(s Spacing) string {
	if s == "" {
		return ""
	}
	return "gap-" + string(s)
}

// classGapX / classGapY emit axis-specific gap utilities.
func classGapX(s Spacing) string {
	if s == "" {
		return ""
	}
	return "gap-x-" + string(s)
}

func classGapY(s Spacing) string {
	if s == "" {
		return ""
	}
	return "gap-y-" + string(s)
}

// classWidth maps Spacing → "w-*".
func classWidth(s Spacing) string {
	if s == "" {
		return ""
	}
	return "w-" + string(s)
}

// classHeight maps Spacing → "h-*".
func classHeight(s Spacing) string {
	if s == "" {
		return ""
	}
	return "h-" + string(s)
}

func classHeightViewport(v ViewportHeight) string {
	if v == "" {
		return ""
	}
	return "h-[" + string(v) + "vh]"
}

func classMaxHeightViewport(v ViewportHeight) string {
	if v == "" {
		return ""
	}
	return "max-h-[" + string(v) + "vh]"
}

// classMinWidth maps Spacing → "min-w-*".
func classMinWidth(s Spacing) string {
	if s == "" {
		return ""
	}
	return "min-w-" + string(s)
}

// classMinHeight maps Spacing → "min-h-*".
func classMinHeight(s Spacing) string {
	if s == "" {
		return ""
	}
	return classPrefixMinH + string(s)
}

// classSpaceX / classSpaceY emit "space-x-<Spacing>" / "space-y-<Spacing>"
// — Tailwind's adjacent-sibling spacing utility.
func classSpaceX(s Spacing) string {
	if s == "" {
		return ""
	}
	return "space-x-" + string(s)
}

func classSpaceY(s Spacing) string {
	if s == "" {
		return ""
	}
	return "space-y-" + string(s)
}

func classMaxW(v string) string { return prefixOrEmpty(v, "max-w-") }

func classMaxH(v string) string { return prefixOrEmpty(v, "max-h-") }

// classNegMargin emits "-m{side}-{step}" — negative margin on one side.
// Side is one of: "t"/"b"/"l"/"r"/"x"/"y"/"" (all).
func classNegMargin(side string, s Spacing) string {
	if s == "" {
		return ""
	}
	if side == "" {
		return "-m-" + string(s)
	}
	return "-m" + side + "-" + string(s)
}

// classNegTop / classNegRight / classNegBottom / classNegLeft emit
// "-top-<step>" / "-right-<step>" / etc. — negative positional
// offsets on the Spacing scale. Empty Spacing → empty string.
func classNegTop(s Spacing) string {
	if s == "" {
		return ""
	}
	return "-top-" + string(s)
}

func classNegRight(s Spacing) string {
	if s == "" {
		return ""
	}
	return "-right-" + string(s)
}

func classNegBottom(s Spacing) string {
	if s == "" {
		return ""
	}
	return "-bottom-" + string(s)
}

func classNegLeft(s Spacing) string {
	if s == "" {
		return ""
	}
	return "-left-" + string(s)
}

// classLeftRaw / classRightRaw / classTopRaw / classBottomRaw emit
// fractional or arbitrary-value positional offsets (e.g., "left-1/2",
// "top-full") not expressible via the Spacing scale.
func classLeftRaw(v string) string {
	if v == "" {
		return ""
	}
	return "left-" + v
}

func classRightRaw(v string) string {
	if v == "" {
		return ""
	}
	return "right-" + v
}

func classTopRaw(v string) string {
	if v == "" {
		return ""
	}
	return "top-" + v
}

func classBottomRaw(v string) string {
	if v == "" {
		return ""
	}
	return "bottom-" + v
}
