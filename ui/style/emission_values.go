package style

// values.go owns the CSS values behind tw's non-color enumerations. Class
// names follow Tailwind conventions, so the values follow Tailwind's
// documented scales; where a tw constant documents its value in a comment
// (spacing, weights), these tables match that comment exactly.

import (
	"strconv"
	"strings"
)

// spacingCSS translates a Spacing suffix ("px", "0", "4", …) into a CSS
// length: px → 1px, n → n×0.25rem. Returns ok=false for unknown suffixes.
func spacingCSS(suffix string) (string, bool) {
	switch suffix {
	case "px":
		return "1px", true
	case "0":
		return "0px", true
	// tw's Spacing type reaches every spacing-valued method, so these travel
	// wherever the builder can put them; "auto" on a property that rejects it
	// is dropped by the browser, mirroring tw's own permissiveness.
	case "auto":
		return "auto", true
	case "full":
		return "100%", true
	case "0.5":
		return "0.125rem", true
	case "1.5":
		return "0.375rem", true
	case "2.5":
		return "0.625rem", true
	case "3.5":
		return "0.875rem", true
	}
	n, err := strconv.Atoi(suffix)
	if err != nil || n < 0 {
		return "", false
	}
	// n×0.25rem, rendered without trailing zeros (4 → 1rem, 2 → 0.5rem).
	whole, quarter := n/4, n%4
	switch quarter {
	case 0:
		return strconv.Itoa(whole) + "rem", true
	case 2:
		if whole == 0 {
			return "0.5rem", true
		}
		return strconv.Itoa(whole) + ".5rem", true
	default: // .25 or .75
		return strconv.Itoa(whole) + "." + strconv.Itoa(quarter*25) + "rem", true
	}
}

// translateCSS translates a Translate suffix, which unlike Spacing carries
// halves and an explicit negative form ("neg-1" → -0.25rem).
func translateCSS(suffix string) (string, bool) {
	neg := strings.HasPrefix(suffix, "neg-")
	s := strings.TrimPrefix(suffix, "neg-")
	var v string
	switch s {
	case "px":
		v = "1px"
	case "0":
		return "0px", true
	case "0.5":
		v = "0.125rem"
	case "1/2":
		v = "50%"
	default:
		var ok bool
		if v, ok = spacingCSS(s); !ok {
			return "", false
		}
	}
	if neg {
		v = "-" + v
	}
	return v, true
}

var fontSizes = map[string][2]string{ // size, line-height
	"xs": {"0.75rem", "1rem"}, "sm": {"0.875rem", "1.25rem"},
	"base": {"1rem", "1.5rem"}, "lg": {"1.125rem", "1.75rem"},
	"xl": {"1.25rem", "1.75rem"}, "2xl": {"1.5rem", "2rem"},
	"3xl": {"1.875rem", "2.25rem"}, "4xl": {"2.25rem", "2.5rem"},
	"5xl": {"3rem", "1"}, "6xl": {"3.75rem", "1"}, "7xl": {"4.5rem", "1"},
	"8xl": {"6rem", "1"}, "9xl": {"8rem", "1"},
}

var fontWeights = map[string]string{
	"thin": "100", "extralight": "200", "light": "300", "normal": "400",
	"medium": "500", "semibold": "600", "bold": "700", "extrabold": "800",
	"black": "900",
}

// Font stacks resolve to the theme's type tokens: the body face is the sans
// stack, the display face is the serif stack, and mono is mono.
var fontFamilies = map[string]string{
	"sans":  "var(--pk-font-body)",
	"serif": "var(--pk-font-display)",
	"mono":  "var(--pk-font-mono)",
}

var trackings = map[string]string{
	"tighter": "-0.05em", "tight": "-0.025em", "normal": "0em",
	"wide": "0.025em", "wider": "0.05em", "widest": "0.1em",
}

var leadings = map[string]string{
	"none": "1", "tight": "1.25", "snug": "1.375", "normal": "1.5",
	"relaxed": "1.625", "loose": "2",
}

// radii: the "base" constant compiles to a bare "rounded" (empty suffix).
var radii = map[string]string{
	"none": "0px", "sm": "0.125rem", "": "0.25rem", "md": "0.375rem",
	"lg": "0.5rem", "xl": "0.75rem", "2xl": "1rem", "3xl": "1.5rem",
	"full": "9999px",
}

// shadows: the "base" constant compiles to a bare "shadow" (empty suffix).
var shadows = map[string]string{
	"sm":    "0 1px 2px 0 rgb(0 0 0 / 0.05)",
	"":      "0 1px 3px 0 rgb(0 0 0 / 0.1), 0 1px 2px -1px rgb(0 0 0 / 0.1)",
	"md":    "0 4px 6px -1px rgb(0 0 0 / 0.1), 0 2px 4px -2px rgb(0 0 0 / 0.1)",
	"lg":    "0 10px 15px -3px rgb(0 0 0 / 0.1), 0 4px 6px -4px rgb(0 0 0 / 0.1)",
	"xl":    "0 20px 25px -5px rgb(0 0 0 / 0.1), 0 8px 10px -6px rgb(0 0 0 / 0.1)",
	"2xl":   "0 25px 50px -12px rgb(0 0 0 / 0.25)",
	"inner": "inset 0 2px 4px 0 rgb(0 0 0 / 0.05)",
	"none":  "0 0 #0000",
}

var maxWidths = map[string]string{
	"xs": "20rem", "sm": "24rem", "md": "28rem", "lg": "32rem", "xl": "36rem",
	"2xl": "42rem", "3xl": "48rem", "4xl": "56rem", "5xl": "64rem",
	"6xl": "72rem", "7xl": "80rem", "prose": "65ch", "full": "100%",
	"none": "none", "screen": "100vw",
}

var easings = map[string]string{
	"linear": "linear", "in": "cubic-bezier(0.4, 0, 1, 1)",
	"out": "cubic-bezier(0, 0, 0.2, 1)", "in-out": "cubic-bezier(0.4, 0, 0.2, 1)",
}

// transitions: the property groups behind transition-<kind>. Every group
// shares the standard duration/easing defaults; duration-* and ease-*
// override them via the custom properties set here.
var transitionProps = map[string]string{
	"none": "none",
	"all":  "all",
	"colors": "color, background-color, border-color, " +
		"text-decoration-color, fill, stroke",
	"opacity":   "opacity",
	"shadow":    "box-shadow",
	"transform": "transform",
}

var breakpoints = map[string]string{
	"sm": "640px", "md": "768px", "lg": "1024px", "xl": "1280px", "2xl": "1536px",
}

// stateSelector renders the selector for one State prefix applied to an
// escaped class selector. Returns ok=false for states this emitter does not
// support (see resolve.go for the fail-closed contract).
func stateSelector(state, escapedClass string) (string, bool) {
	simple := map[string]string{
		"hover": ":hover", "focus": ":focus", "focus-visible": ":focus-visible",
		"focus-within": ":focus-within", "active": ":active",
		"disabled": ":disabled", "checked": ":checked",
		"first": ":first-child", "last": ":last-child",
		"odd": ":nth-child(odd)", "even": ":nth-child(even)",
	}
	if suffix, ok := simple[state]; ok {
		return "." + escapedClass + suffix, true
	}
	switch state {
	case "placeholder":
		return "." + escapedClass + "::placeholder", true
	case "group-hover":
		return ".group:hover ." + escapedClass, true
	case "group-focus":
		return ".group:focus-within ." + escapedClass, true
	case "dark":
		return ".dark ." + escapedClass, true
	}
	return "", false
}

// escapeClass escapes the characters legal in tw class names but not in CSS
// identifiers, so a compiled class can appear verbatim in a selector.
func escapeClass(class string) string {
	r := strings.NewReplacer(":", "\\:", "/", "\\/", "[", "\\[", "]", "\\]", ".", "\\.")
	return r.Replace(class)
}
