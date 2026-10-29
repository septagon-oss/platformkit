package style

// classRounded maps Radius → "rounded-*".
func classRounded(r Radius) string {
	if r == "" {
		return ""
	}
	if r == RadiusBase {
		return "rounded"
	}
	return classPrefixRounded + string(r)
}

// classRoundedSide emits a per-side border-radius utility,
// e.g. "rounded-t", "rounded-l-lg". Side is one of "t"/"b"/"l"/"r".
func classRoundedSide(side string, r Radius) string {
	if side == "" {
		return classRounded(r)
	}
	if r == "" {
		return ""
	}
	if r == RadiusBase {
		return classPrefixRounded + side
	}
	return classPrefixRounded + side + "-" + string(r)
}

// classShadow maps Shadow → "shadow-*".
func classShadow(sh Shadow) string {
	if sh == "" {
		return ""
	}
	if sh == ShadowBase {
		return "shadow"
	}
	return "shadow-" + string(sh)
}

// classTransition maps Transition → "transition-*".
func classTransition(t Transition) string {
	if t == "" {
		return ""
	}
	if t == TransitionAll {
		return "transition-all"
	}
	return "transition-" + string(t)
}

// classDuration maps Duration → "duration-*".
func classDuration(d Duration) string {
	if d == "" {
		return ""
	}
	return "duration-" + string(d)
}

// classEasing maps Easing → "ease-*".
func classEasing(e Easing) string {
	if e == "" {
		return ""
	}
	return "ease-" + string(e)
}

// classBorderWidth maps BorderWidth → "border-*".
func classBorderWidth(b BorderWidth) string {
	if b == Border1 {
		return "border"
	}
	return classPrefixBorder + string(b)
}

// classBorderSide emits a side-specific border width utility,
// e.g. "border-t", "border-l-2".
func classBorderSide(side string, b BorderWidth) string {
	if side == "" {
		return classBorderWidth(b)
	}
	if b == Border1 {
		return classPrefixBorder + side
	}
	return classPrefixBorder + side + "-" + string(b)
}

// classRingWidth maps RingWidth → "ring-*". Every width emits
// "ring-<N>" — including Ring1 which emits "ring-1". (Tailwind's bare
// "ring" utility defaults to 3px, which is different from Ring1.)
func classRingWidth(r RingWidth) string {
	if r == "" {
		return ""
	}
	return "ring-" + string(r)
}

// classRingOffset maps RingOffset → "ring-offset-*".
func classRingOffset(r RingOffset) string {
	if r == "" {
		return ""
	}
	return "ring-offset-" + string(r)
}

// classOpacity maps Opacity → "opacity-*".
func classOpacity(o Opacity) string {
	if o == "" {
		return ""
	}
	return "opacity-" + string(o)
}

// classCursor maps Cursor → "cursor-*".
func classCursor(c Cursor) string {
	if c == "" {
		return ""
	}
	return "cursor-" + string(c)
}

// classPointerEvents maps PointerEvents → "pointer-events-*".
func classPointerEvents(p PointerEvents) string {
	if p == "" {
		return ""
	}
	return "pointer-events-" + string(p)
}

// classOutline maps Outline → "outline-*".
func classOutline(o Outline) string {
	if o == "" {
		return ""
	}
	return "outline-" + string(o)
}

// classBorderStyle maps BorderStyle → "border-<style>".
func classBorderStyle(b BorderStyle) string {
	if b == "" {
		return ""
	}
	return classPrefixBorder + string(b)
}

// classResize maps ResizeMode → "resize" / "resize-*".
// ResizeModeBoth emits bare "resize" (Tailwind's default resize
// utility); all other values use the "resize-<mode>" form.
func classResize(m ResizeMode) string {
	if m == "" {
		return ""
	}
	if m == ResizeModeBoth {
		return "resize"
	}
	return "resize-" + string(m)
}

// classSelect maps Select → "select-*".
func classSelect(s Select) string {
	if s == "" {
		return ""
	}
	return "select-" + string(s)
}

// classTranslateY maps Translate → "translate-y-*" or "-translate-y-*".
func classTranslateY(t Translate) string {
	if t == "" {
		return ""
	}
	switch t {
	case TranslateNeg05:
		return "-translate-y-0.5"
	case TranslateNegHalf:
		return "-translate-y-1/2"
	case TranslateNeg1:
		return "-translate-y-1"
	case TranslateNeg2:
		return "-translate-y-2"
	default:
		return classPrefixTranslateY + string(t)
	}
}

// classTranslateX maps Translate → "translate-x-*" or "-translate-x-*".
func classTranslateX(t Translate) string {
	if t == "" {
		return ""
	}
	switch t {
	case TranslateNeg05:
		return "-translate-x-0.5"
	case TranslateNegHalf:
		return "-translate-x-1/2"
	case TranslateNeg1:
		return "-translate-x-1"
	case TranslateNeg2:
		return "-translate-x-2"
	default:
		return classPrefixTranslateX + string(t)
	}
}

// classTranslateXSpacing / classTranslateYSpacing emit translate-x-<Spacing>
// using the Spacing scale. Distinct from classTranslateX (which uses the
// Translate enum with its neg-* prefixes) — these are for forward offsets
// driven by tokens.
func classTranslateXSpacing(s Spacing) string {
	if s == "" {
		return ""
	}
	return classPrefixTranslateX + string(s)
}

func classTranslateYSpacing(s Spacing) string {
	if s == "" {
		return ""
	}
	return classPrefixTranslateY + string(s)
}

// classRotate emits "rotate-<deg>" or "-rotate-<deg>".
func classRotate(deg string) string {
	if deg == "" {
		return ""
	}
	return "rotate-" + deg
}

// classDivideX / classDivideY emit "divide-x-<step>" / "divide-y-<step>"
// for borders between adjacent children.
func classDivideX(s Spacing) string {
	if s == "" {
		return "divide-x"
	}
	return "divide-x-" + string(s)
}

func classDivideY(s Spacing) string {
	if s == "" {
		return "divide-y"
	}
	return "divide-y-" + string(s)
}
