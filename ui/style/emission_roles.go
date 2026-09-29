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
//
// A mix toward a *surface* is only legible on that surface: it walks the
// foreground toward the background it is painted on, so the same role falls
// below the contrast floor the moment a component puts it on another one
// (mixing text 60% into surface-primary measures 4.35:1 on surface-primary and
// 3.01:1 on surface-muted). Body text therefore mixes between two colours the
// theme's own gate certifies on every surface — text-primary and text-muted —
// which keeps the tone between two certified ratios instead of walking it off
// the palette. BodyRolePairs measures the result rather than trusting it.
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
		SurfaceBrandSoft:   mix(accent, design.SoftTintPercent, surfacePrimary),
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
		FgPrimary:   textPrimary,
		FgSecondary: mix(textPrimary, 66, textMuted),
		FgTertiary:  mix(textPrimary, 33, textMuted),
		FgMuted:     textMuted,
		// A placeholder is the label of an empty field: a user reads it to decide
		// what to type, so it is body text and not exempt the way a disabled
		// control is. It used to be text-muted walked 30% toward the surface under
		// it, which measured as low as 1.62:1 on a raised card.
		FgPlaceholder: textMuted,
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

// bodyRole is one foreground role and one surface role the kernel's components
// compose at body size, as a design.RolePair.
type bodyRole = design.RolePair

// bodyRolePairs are the text pairs ui/components actually paints, read out of
// classlists.go: the neutral badge and the read-only field put FgSecondary on
// SurfaceTertiary, a detail description puts it on SurfaceSecondary, the brand
// text utility, the outline and link button variants and a brand detail value put
// the accent on whatever surface the card around them raised itself onto, and
// every one of them is body-size copy. FgDisabled is deliberately absent: a
// disabled control is the one text WCAG 2.2 SC 1.4.3 exempts, and a gate that
// measured it would demand a disabled field look enabled.
var bodyRolePairs = []design.RolePair{
	{Foreground: "--pk-role-fg-primary", Background: "--pk-role-surface-primary", Min: design.MinContrast},
	{Foreground: "--pk-role-fg-primary", Background: "--pk-role-surface-secondary", Min: design.MinContrast},
	{Foreground: "--pk-role-fg-primary", Background: "--pk-role-surface-tertiary", Min: design.MinContrast},
	{Foreground: "--pk-role-fg-on-surface", Background: "--pk-role-surface-primary", Min: design.MinContrast},
	{Foreground: "--pk-role-fg-on-surface", Background: "--pk-role-surface-tertiary", Min: design.MinContrast},
	{Foreground: "--pk-role-fg-secondary", Background: "--pk-role-surface-primary", Min: design.MinContrast},
	{Foreground: "--pk-role-fg-secondary", Background: "--pk-role-surface-secondary", Min: design.MinContrast},
	{Foreground: "--pk-role-fg-secondary", Background: "--pk-role-surface-tertiary", Min: design.MinContrast},
	{Foreground: "--pk-role-fg-tertiary", Background: "--pk-role-surface-primary", Min: design.MinContrast},
	{Foreground: "--pk-role-fg-tertiary", Background: "--pk-role-surface-secondary", Min: design.MinContrast},
	{Foreground: "--pk-role-fg-tertiary", Background: "--pk-role-surface-tertiary", Min: design.MinContrast},
	{Foreground: "--pk-role-fg-muted", Background: "--pk-role-surface-primary", Min: design.MinContrast},
	{Foreground: "--pk-role-fg-muted", Background: "--pk-role-surface-secondary", Min: design.MinContrast},
	{Foreground: "--pk-role-fg-muted", Background: "--pk-role-surface-tertiary", Min: design.MinContrast},
	{Foreground: "--pk-role-fg-placeholder", Background: "--pk-role-surface-primary", Min: design.MinContrast},
	{Foreground: "--pk-role-fg-placeholder", Background: "--pk-role-surface-secondary", Min: design.MinContrast},
	{Foreground: "--pk-role-fg-placeholder", Background: "--pk-role-surface-tertiary", Min: design.MinContrast},
	// The accent as copy a reader reads. clTextColor["brand"],
	// clDetailValueTone["brand"] and the outline button variant paint FgBrand with
	// no background of their own, clLink paints FgLink (the same colour, in a
	// sentence) and its hover state paints FgLinkHover, so all three land on
	// whichever surface the card around them raised itself onto.
	{Foreground: "--pk-role-fg-brand", Background: "--pk-role-surface-primary", Min: design.MinContrast},
	{Foreground: "--pk-role-fg-brand", Background: "--pk-role-surface-secondary", Min: design.MinContrast},
	{Foreground: "--pk-role-fg-brand", Background: "--pk-role-surface-tertiary", Min: design.MinContrast},
	{Foreground: "--pk-role-fg-link", Background: "--pk-role-surface-primary", Min: design.MinContrast},
	{Foreground: "--pk-role-fg-link", Background: "--pk-role-surface-secondary", Min: design.MinContrast},
	{Foreground: "--pk-role-fg-link", Background: "--pk-role-surface-tertiary", Min: design.MinContrast},
	{Foreground: "--pk-role-fg-link-hover", Background: "--pk-role-surface-primary", Min: design.MinContrast},
	{Foreground: "--pk-role-fg-link-hover", Background: "--pk-role-surface-secondary", Min: design.MinContrast},
	{Foreground: "--pk-role-fg-link-hover", Background: "--pk-role-surface-tertiary", Min: design.MinContrast},
	{Foreground: "--pk-role-fg-on-inverse", Background: "--pk-role-surface-inverse", Min: design.MinContrast},
}

// BodyRolePairs returns the body-size text pairs this package's roles compose,
// for design.Theme.CheckRoles. A caller that emits a stylesheet hands these over
// with RoleColors: the token gate says what a theme sets, these say what a reader
// is shown.
func BodyRolePairs() []design.RolePair {
	return slices.Clone(bodyRolePairs)
}

// tintedRolePairs names the body pairs whose background this layer invents
// rather than takes from the theme: --pk-role-surface-brand-soft is
// design.SoftTintPercent of the accent mixed into the card surface, and
// clBadgeVariant["primary"], clBadgeTone["brand"], clSidebarLinkActiveContent and
// the outline button's hover state all paint that accent's own colour on it. A
// token pair cannot express it, because the background is no token, and the mix
// always reads worse than the surface it was taken from — which is why
// design.enforceTinted certifies a generated accent against it; this list is
// gated at ui/export, where a Pair becomes a sheet, not at design.Client.Resolve.
var tintedRolePairs = []design.RolePair{
	{Foreground: "--pk-role-fg-brand", Background: "--pk-role-surface-brand-soft", Min: design.MinContrast},
	{Foreground: "--pk-role-fg-link", Background: "--pk-role-surface-brand-soft", Min: design.MinContrast},
}

// TintedRolePairs returns the body pairs whose background this layer derives by
// mixing a foreground into a surface. Whoever gates the layer a browser paints
// hands over BodyRolePairs and these: those are what a reader is shown on a
// theme's own surfaces, these what a reader is shown on a surface this layer
// derives from a theme's.
func TintedRolePairs() []design.RolePair {
	return slices.Clone(tintedRolePairs)
}

// roleVar renders the var() reference utility rules use for a color role.
func roleVar(c Color) string { return "var(--pk-role-" + string(c) + ")" }
