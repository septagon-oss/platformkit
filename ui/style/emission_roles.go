package style

// emission_roles.go owns the single mapping from semantic color roles to the
// design system's theme tokens. Utility rules never reference theme tokens
// directly: they reference --pk-role-* variables, and this file emits those
// variables from --pk-* token variables (or derives them with color-mix when
// the theme has no dedicated token). Retheming therefore never touches the
// utility rules — a different theme changes the values behind the same roles.

import (
	"maps"
	"slices"

	"github.com/septagon-oss/platformkit/design"
)

func tokenVar(path string) design.ColorValue {
	return design.ColorValue{Reference: "--pk-color-" + path}
}

// mix records pct% of colorA mixed with its complement of colorB, in sRGB.
// Used for the soft, hover, and disabled roles the theme does not enumerate.
func mix(a design.ColorValue, pct float64, b design.ColorValue) design.ColorValue {
	return design.ColorValue{Mix: &design.ColorMix{First: a, FirstPercent: pct, Second: b}}
}

// roleValues maps every Color to its source value in terms of theme token
// variables. TestRoleMapCoversEveryColor pins this to AllColors(), so a new
// role fails this package's tests until it is mapped here.
func roleValues() map[Color]design.ColorValue {
	surfacePrimary := tokenVar("surface-primary")
	textPrimary := tokenVar("text-primary")
	textMuted := tokenVar("text-muted")
	accent := tokenVar("accent-default")
	focus := tokenVar("focus")

	return map[Color]design.ColorValue{
		// Surfaces.
		SurfacePrimary:     surfacePrimary,
		SurfaceSecondary:   tokenVar("surface-canvas"),
		SurfaceTertiary:    tokenVar("surface-muted"),
		SurfaceBrand:       accent,
		SurfaceBrandHover:  tokenVar("accent-hover"),
		SurfaceBrandSoft:   mix(accent, 12, surfacePrimary),
		SurfaceSuccess:     tokenVar("status-ok"),
		SurfaceSuccessSoft: tokenVar("status-okbg"),
		SurfaceWarning:     tokenVar("status-warning"),
		SurfaceWarningSoft: tokenVar("status-warningbg"),
		SurfaceDanger:      tokenVar("status-danger"),
		SurfaceDangerSoft:  tokenVar("status-dangerbg"),
		SurfaceInfo:        tokenVar("status-info"),
		SurfaceInfoSoft:    tokenVar("status-infobg"),
		SurfaceDisabled:    tokenVar("surface-muted"),
		SurfaceHover:       mix(textPrimary, 4, surfacePrimary),
		SurfaceActive:      mix(textPrimary, 8, surfacePrimary),
		SurfaceOverlay:     mix(tokenVar("sidebar-bg"), 55, design.ColorValue{Literal: "transparent"}),
		SurfaceInverse:     tokenVar("sidebar-bg"),

		// Foreground.
		FgPrimary:     textPrimary,
		FgSecondary:   mix(textPrimary, 78, surfacePrimary),
		FgTertiary:    mix(textPrimary, 60, surfacePrimary),
		FgMuted:       textMuted,
		FgPlaceholder: mix(textMuted, 70, surfacePrimary),
		FgBrand:       accent,
		FgOnBrand:     tokenVar("accent-on"),
		FgSuccess:     tokenVar("status-ok"),
		FgWarning:     tokenVar("status-warning"),
		FgDanger:      tokenVar("status-danger"),
		FgInfo:        tokenVar("status-info"),
		FgDisabled:    mix(textMuted, 55, surfacePrimary),
		FgOnSurface:   textPrimary,
		FgOnInverse:   tokenVar("sidebar-text"),
		FgLink:        accent,
		FgLinkHover:   tokenVar("accent-hover"),

		// Borders.
		BorderPrimary:   tokenVar("border-default"),
		BorderSecondary: mix(tokenVar("border-default"), 60, surfacePrimary),
		BorderBrand:     accent,
		BorderSuccess:   tokenVar("status-ok"),
		BorderWarning:   tokenVar("status-warning"),
		BorderDanger:    tokenVar("status-danger"),
		BorderInfo:      tokenVar("status-info"),

		// Rings.
		RingBrand:  accent,
		RingFocus:  focus,
		RingDanger: tokenVar("status-danger"),

		// Neutrals.
		ColorTransparent: {Literal: "transparent"},
		ColorWhite:       {Literal: "#ffffff"},
		ColorBlack:       {Literal: "#000000"},
	}
}

// RoleColors projects fresh, ordered declarations from the same source values
// as RoleVars. Resolve them with one selected Theme.Tokens() using
// design.ResolveColors; no mode, palette or native support is inferred here.
func RoleColors() []design.ColorToken {
	roles := roleValues()
	out := make([]design.ColorToken, 0, len(roles))
	for _, name := range slices.Sorted(maps.Keys(roles)) {
		out = append(out, design.ColorToken{Name: "--pk-role-" + string(name), Value: roles[name]})
	}
	return out
}

// roleVar renders the var() reference utility rules use for a color role.
func roleVar(c Color) string { return "var(--pk-role-" + string(c) + ")" }
