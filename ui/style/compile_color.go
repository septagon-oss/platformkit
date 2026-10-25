package style

// classBg maps a Color const to its "bg-*" Tailwind class.
//
// Every non-zero Color yields a "bg-<semantic-name>" class. A previous
// partial switch only covered a subset (mainly surface + a handful of
// fg colors) which meant tw.Bg(FgDisabled), tw.Bg(FgSuccess), etc.
// silently returned empty — leading to dot-indicator slots rendering
// with no background. The uniform `bg-` + string() mapping below makes
// Bg() total over the Color domain, matching classText's behaviour.
func classBg(c Color) string {
	if c == "" {
		return ""
	}
	if c == ColorTransparent {
		return "bg-transparent"
	}
	return "bg-" + string(c)
}

// classText maps a Color const to its "text-*" Tailwind class.
// Any semantic color may be applied as a text color when the design
// treats it as such (e.g., using surface-brand as the tick color on a
// checkbox). Surface/Border/Ring colors are accepted in addition to
// the Fg family.
func classText(c Color) string {
	if c == "" {
		return ""
	}
	if c == ColorTransparent {
		return "text-transparent"
	}
	return "text-" + string(c)
}

// classBorder maps a Color const to its "border-*" Tailwind class.
func classBorder(c Color) string {
	switch c {
	case ColorTransparent:
		return "border-transparent"
	case BorderPrimary:
		return "border-border-primary"
	case BorderSecondary:
		return "border-border-secondary"
	case BorderBrand:
		return "border-border-brand"
	case BorderSuccess:
		return "border-border-success"
	case BorderWarning:
		return "border-border-warning"
	case BorderDanger:
		return "border-border-danger"
	case BorderInfo:
		return "border-border-info"
	case SurfaceBrand:
		return "border-surface-brand"
	}
	return ""
}

// classRing maps a Color const to its "ring-*" Tailwind class.
// Any semantic color may be used as a ring color; the named Ring*
// constants (RingBrand/RingFocus/RingDanger) are the canonical
// focus-ring palette, but Border* and other colors are accepted when
// the design uses a non-focus ring.
func classRing(c Color) string {
	if c == "" {
		return ""
	}
	switch c {
	case RingBrand:
		return "ring-ring-brand"
	case RingFocus:
		return "ring-ring-focus"
	case RingDanger:
		return "ring-ring-danger"
	}
	return "ring-" + string(c)
}

// classBorderSideColor emits a per-side border-color utility,
// e.g. "border-t-white", "border-l-border-primary".
func classBorderSideColor(side string, c Color) string {
	if side == "" || c == "" {
		return ""
	}
	return classPrefixBorder + side + "-" + string(c)
}

// classBgWithOpacity emits "bg-<color>/<opacity>" for alpha-blended
// backgrounds (e.g., "bg-surface-overlay/75").
func classBgWithOpacity(c Color, opacity string) string {
	if c == "" || opacity == "" {
		return ""
	}
	return "bg-" + string(c) + "/" + opacity
}

// classAccent emits "accent-<color>" for native form chrome tinting.
func classAccent(c Color) string {
	if c == "" {
		return ""
	}
	return "accent-" + string(c)
}
