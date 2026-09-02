package style

// roles.go owns the single mapping from tw's semantic color roles to the
// design system's theme tokens. Utility rules never reference theme tokens
// directly: they reference --pk-role-* variables, and this file emits those
// variables from --pk-* token variables (or derives them with color-mix when
// the theme has no dedicated token). Retheming therefore never touches the
// utility rules — a different theme changes the values behind the same roles.

import ()

// tokenVar renders a var() reference to a pk-design token custom property.
func tokenVar(path string) string { return "var(--pk-color-" + path + ")" }

// mix derives a tint: pct% of colorA over colorB, in sRGB. Used for the soft,
// hover, and disabled roles the theme intentionally does not enumerate.
func mix(a string, pct string, b string) string {
	return "color-mix(in srgb, " + a + " " + pct + "%, " + b + ")"
}

// roleValues maps every Color to its CSS value in terms of theme token
// variables. TestRoleMapCoversEveryColor pins this to AllColors(), so a new
// role in tw fails the build of this package's tests until it is mapped here.
func roleValues() map[Color]string {
	surfacePrimary := tokenVar("surface-primary")
	textPrimary := tokenVar("text-primary")
	textMuted := tokenVar("text-muted")
	accent := tokenVar("accent-default")
	focus := tokenVar("focus")

	return map[Color]string{
		// Surfaces.
		SurfacePrimary:     surfacePrimary,
		SurfaceSecondary:   tokenVar("surface-canvas"),
		SurfaceTertiary:    tokenVar("surface-muted"),
		SurfaceBrand:       accent,
		SurfaceBrandHover:  tokenVar("accent-hover"),
		SurfaceBrandSoft:   mix(accent, "12", surfacePrimary),
		SurfaceSuccess:     tokenVar("status-ok"),
		SurfaceSuccessSoft: tokenVar("status-okbg"),
		SurfaceWarning:     tokenVar("status-warning"),
		SurfaceWarningSoft: tokenVar("status-warningbg"),
		SurfaceDanger:      tokenVar("status-danger"),
		SurfaceDangerSoft:  tokenVar("status-dangerbg"),
		SurfaceInfo:        tokenVar("status-info"),
		SurfaceInfoSoft:    tokenVar("status-infobg"),
		SurfaceDisabled:    tokenVar("surface-muted"),
		SurfaceHover:       mix(textPrimary, "4", surfacePrimary),
		SurfaceActive:      mix(textPrimary, "8", surfacePrimary),
		SurfaceOverlay:     mix(tokenVar("sidebar-bg"), "55", "transparent"),
		SurfaceInverse:     tokenVar("sidebar-bg"),

		// Foreground.
		FgPrimary:     textPrimary,
		FgSecondary:   mix(textPrimary, "78", surfacePrimary),
		FgTertiary:    mix(textPrimary, "60", surfacePrimary),
		FgMuted:       textMuted,
		FgPlaceholder: mix(textMuted, "70", surfacePrimary),
		FgBrand:       accent,
		FgOnBrand:     tokenVar("accent-on"),
		FgSuccess:     tokenVar("status-ok"),
		FgWarning:     tokenVar("status-warning"),
		FgDanger:      tokenVar("status-danger"),
		FgInfo:        tokenVar("status-info"),
		FgDisabled:    mix(textMuted, "55", surfacePrimary),
		FgOnSurface:   textPrimary,
		FgOnInverse:   tokenVar("sidebar-text"),
		FgLink:        accent,
		FgLinkHover:   tokenVar("accent-hover"),

		// Borders.
		BorderPrimary:   tokenVar("border-default"),
		BorderSecondary: mix(tokenVar("border-default"), "60", surfacePrimary),
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
		ColorTransparent: "transparent",
		ColorWhite:       "#ffffff",
		ColorBlack:       "#000000",
	}
}

// roleVar renders the var() reference utility rules use for a color role.
func roleVar(c Color) string { return "var(--pk-role-" + string(c) + ")" }
