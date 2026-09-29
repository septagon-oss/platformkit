package design

// roles.go owns the semantic colour layer: the --pk-role-* declarations a
// stylesheet paints with, in terms of the twenty-two tokens a theme sets, and
// the pairs of those roles a reader is actually shown. It lives here rather
// than with the layer that renders it because this package is the one that
// measures a ratio: a gate that could read only the token layer could certify a
// palette nobody paints with, which is how a theme that passes Check still
// paints a sentence nobody can read — a component never sets
// --pk-color-text-primary, it sets color: var(--pk-role-fg-secondary).
//
// The declarations are data: names, references and mixes, no SDK, no CSS text,
// no rendering rule, so this file keeps the package's dependency rule (the
// standard library alone) while ui/style renders these declarations into the
// :root block and ui/export projects them into the token document a native
// application reads. The role names are the ones those layers emit; nothing is
// derived at render time, so the pair Client.Resolve refuses and the pair
// ui/export refuses are the same pair read from one list.

import (
	"maps"
	"slices"
)

// roleRef names the theme token a role reads.
func roleRef(token string) ColorValue {
	return ColorValue{Reference: "--pk-color-" + token}
}

// roleMix records pct% of first mixed with its complement of second, in sRGB.
// It is how the layer derives the soft, hover and disabled roles a theme does
// not enumerate.
//
// A mix toward a *surface* is only legible on that surface: it walks the
// foreground toward the background it is painted on, so the same role falls
// below the contrast floor the moment a component puts it on another one
// (mixing text 66% into surface-primary measures 4.35:1 on surface-primary and
// 3.01:1 on surface-muted). Body text therefore mixes between two colours the
// theme's own gate certifies on every surface — text-primary and text-muted —
// which keeps the tone between two certified ratios instead of walking it off
// the palette. bodyRolePairs measures the result rather than trusting it.
func roleMix(first ColorValue, pct float64, second ColorValue) ColorValue {
	return ColorValue{Mix: &ColorMix{First: first, FirstPercent: pct, Second: second}}
}

// roleLayer maps every role name — without its --pk-role- prefix, since that is
// the prefix its renderers emit — to its source value in terms of theme token
// variables. ui/style's TestRoleMapCoversEveryColor pins this set to the roles
// that package compiles rules for, so a new role fails that test until it is
// mapped here and rendered there.
//
// SurfaceActive is the one declared paint no kernel rule composes yet: it is
// declared here because the layer the export and a native application read is
// complete, and gated nowhere because nothing paints it.
var roleLayer = map[string]ColorValue{
	// Surfaces.
	"surface-primary":      roleRef("surface-primary"),
	"surface-secondary":    roleRef("surface-canvas"),
	"surface-tertiary":     roleRef("surface-muted"),
	"surface-brand":        roleRef("accent-default"),
	"surface-brand-hover":  roleRef("accent-hover"),
	"surface-brand-soft":   roleMix(roleRef("accent-default"), SoftTintPercent, roleRef("surface-primary")),
	"surface-success":      roleRef("status-ok"),
	"surface-success-soft": roleRef("status-okbg"),
	"surface-warning":      roleRef("status-warning"),
	"surface-warning-soft": roleRef("status-warningbg"),
	"surface-danger":       roleRef("status-danger"),
	"surface-danger-soft":  roleRef("status-dangerbg"),
	"surface-info":         roleRef("status-info"),
	"surface-info-soft":    roleRef("status-infobg"),
	"surface-disabled":     roleRef("surface-muted"),
	"surface-hover":        roleMix(roleRef("text-primary"), 4, roleRef("surface-primary")),
	"surface-active":       roleMix(roleRef("text-primary"), 8, roleRef("surface-primary")),
	"surface-overlay":      roleMix(roleRef("sidebar-bg"), 55, ColorValue{Literal: "transparent"}),
	"surface-inverse":      roleRef("sidebar-bg"),

	// Foreground.
	"fg-primary": roleRef("text-primary"),
	"fg-secondary": roleMix(
		roleRef("text-primary"), 66, roleRef("text-muted")),
	"fg-tertiary": roleMix(
		roleRef("text-primary"), 33, roleRef("text-muted")),
	"fg-muted": roleRef("text-muted"),
	// A placeholder is the label of an empty field: a user reads it to decide
	// what to type, so it is body text and not exempt the way a disabled
	// control is. It used to be text-muted walked 30% toward the surface under
	// it, which measured as low as 1.62:1 on a raised card.
	"fg-placeholder": roleRef("text-muted"),
	"fg-brand":       roleRef("accent-default"),
	"fg-on-brand":    roleRef("accent-on"),
	"fg-success":     roleRef("status-ok"),
	"fg-warning":     roleRef("status-warning"),
	"fg-danger":      roleRef("status-danger"),
	"fg-info":        roleRef("status-info"),
	"fg-disabled":    roleMix(roleRef("text-muted"), 55, roleRef("surface-primary")),
	"fg-on-surface":  roleRef("text-primary"),
	"fg-on-inverse":  roleRef("sidebar-text"),
	"fg-link":        roleRef("accent-default"),
	"fg-link-hover":  roleRef("accent-hover"),

	// Borders.
	"border-primary":   roleRef("border-default"),
	"border-secondary": roleMix(roleRef("border-default"), 60, roleRef("surface-primary")),
	"border-brand":     roleRef("accent-default"),
	"border-success":   roleRef("status-ok"),
	"border-warning":   roleRef("status-warning"),
	"border-danger":    roleRef("status-danger"),
	"border-info":      roleRef("status-info"),

	// Rings.
	"ring-brand":  roleRef("accent-default"),
	"ring-focus":  roleRef("focus"),
	"ring-danger": roleRef("status-danger"),

	// Neutrals.
	"transparent": {Literal: "transparent"},
	"white":       {Literal: "#ffffff"},
	"black":       {Literal: "#000000"},
}

// RoleLayer returns fresh, ordered declarations for the whole semantic colour
// layer: every role in terms of a theme's own tokens, under the CSS name its
// renderers emit. Resolve them for one selected Theme.Tokens() with
// ResolveColors; no mode, palette or native support is inferred here. ui/style
// renders them as the --pk-role-* block, ui/export projects them, and
// Client.Resolve gates a client's finished pair with them — one declaration,
// read by the door that accepts a client's file and the seam that ships a sheet.
//
// Every returned value is detached from the table, for the reason detach gives:
// a caller that edits what it was handed must not move the next caller's layer.
func RoleLayer() []ColorToken {
	out := make([]ColorToken, 0, len(roleLayer))
	for _, name := range slices.Sorted(maps.Keys(roleLayer)) {
		out = append(out, ColorToken{Name: "--pk-role-" + name, Value: detach(roleLayer[name])})
	}
	return out
}

// detach copies a role's source value, mix included. A mix is a pointer, so one
// table would otherwise be shared by every caller of RoleLayer, and a caller that
// rewrites a percentage in what it was handed — a case measuring a shallower tint
// than the layer declares, an export re-projecting a value — would move the
// declarations every later caller in the process reads, including the gate.
// Building the table afresh per call is the other way to hold this; copying here
// keeps the declaration a single readable list.
func detach(value ColorValue) ColorValue {
	if mix := value.Mix; mix != nil {
		embedded := *mix
		embedded.First = detach(embedded.First)
		embedded.Second = detach(embedded.Second)
		value.Mix = &embedded
	}
	return value
}

// bodyRolePairs are the text pairs a reader is shown on a theme's own surfaces:
// the neutral badge and the read-only field put fg-secondary on surface-tertiary,
// a detail description puts it on surface-secondary, the brand text utility, the
// outline and link button variants and a brand detail value put the accent on
// whichever surface the card around them raised itself onto, and every one of
// them is body-size copy. fg-disabled is deliberately absent: a disabled control
// is the one text WCAG 2.2 SC 1.4.3 exempts, and a gate that measured it would
// demand a disabled field look enabled.
var bodyRolePairs = []RolePair{
	{Foreground: "--pk-role-fg-primary", Background: "--pk-role-surface-primary", Min: MinContrast},
	{Foreground: "--pk-role-fg-primary", Background: "--pk-role-surface-secondary", Min: MinContrast},
	{Foreground: "--pk-role-fg-primary", Background: "--pk-role-surface-tertiary", Min: MinContrast},
	{Foreground: "--pk-role-fg-on-surface", Background: "--pk-role-surface-primary", Min: MinContrast},
	{Foreground: "--pk-role-fg-on-surface", Background: "--pk-role-surface-tertiary", Min: MinContrast},
	{Foreground: "--pk-role-fg-secondary", Background: "--pk-role-surface-primary", Min: MinContrast},
	{Foreground: "--pk-role-fg-secondary", Background: "--pk-role-surface-secondary", Min: MinContrast},
	{Foreground: "--pk-role-fg-secondary", Background: "--pk-role-surface-tertiary", Min: MinContrast},
	{Foreground: "--pk-role-fg-tertiary", Background: "--pk-role-surface-primary", Min: MinContrast},
	{Foreground: "--pk-role-fg-tertiary", Background: "--pk-role-surface-secondary", Min: MinContrast},
	{Foreground: "--pk-role-fg-tertiary", Background: "--pk-role-surface-tertiary", Min: MinContrast},
	{Foreground: "--pk-role-fg-muted", Background: "--pk-role-surface-primary", Min: MinContrast},
	{Foreground: "--pk-role-fg-muted", Background: "--pk-role-surface-secondary", Min: MinContrast},
	{Foreground: "--pk-role-fg-muted", Background: "--pk-role-surface-tertiary", Min: MinContrast},
	{Foreground: "--pk-role-fg-placeholder", Background: "--pk-role-surface-primary", Min: MinContrast},
	{Foreground: "--pk-role-fg-placeholder", Background: "--pk-role-surface-secondary", Min: MinContrast},
	{Foreground: "--pk-role-fg-placeholder", Background: "--pk-role-surface-tertiary", Min: MinContrast},
	// The accent as copy a reader reads: the brand text utility, a brand detail
	// value and the outline button variant paint fg-brand with no background of
	// their own, a link paints fg-link (the same colour, in a sentence) and its
	// hover state paints fg-link-hover, so all three land on whichever surface
	// the card around them raised itself onto.
	{Foreground: "--pk-role-fg-brand", Background: "--pk-role-surface-primary", Min: MinContrast},
	{Foreground: "--pk-role-fg-brand", Background: "--pk-role-surface-secondary", Min: MinContrast},
	{Foreground: "--pk-role-fg-brand", Background: "--pk-role-surface-tertiary", Min: MinContrast},
	{Foreground: "--pk-role-fg-link", Background: "--pk-role-surface-primary", Min: MinContrast},
	{Foreground: "--pk-role-fg-link", Background: "--pk-role-surface-secondary", Min: MinContrast},
	{Foreground: "--pk-role-fg-link", Background: "--pk-role-surface-tertiary", Min: MinContrast},
	{Foreground: "--pk-role-fg-link-hover", Background: "--pk-role-surface-primary", Min: MinContrast},
	{Foreground: "--pk-role-fg-link-hover", Background: "--pk-role-surface-secondary", Min: MinContrast},
	{Foreground: "--pk-role-fg-link-hover", Background: "--pk-role-surface-tertiary", Min: MinContrast},
	{Foreground: "--pk-role-fg-on-inverse", Background: "--pk-role-surface-inverse", Min: MinContrast},
}

// BodyRolePairs returns the body-size text pairs the semantic layer composes on
// a theme's own surfaces, for Theme.CheckRoles. Whoever gates a palette hands
// these over with RoleLayer: the token gate says what a theme sets, these say
// what a reader is shown.
func BodyRolePairs() []RolePair { return slices.Clone(bodyRolePairs) }

// tintedRolePairs names the body pairs whose background this layer invents
// rather than takes from the theme: surface-brand-soft is SoftTintPercent of the
// accent mixed into the card surface, and a primary badge, a brand badge, the
// active sidebar link and the outline button's hover state all paint that
// accent's own colour on it. A token pair cannot express it, because the
// background is no token, and the mix always reads worse than the surface it was
// taken from — which is why enforceTinted certifies a generated accent against
// it, and why Client.Resolve gates a client's override against it too: an
// override of the accent, or of the surface that tint is mixed into, moves that
// badge's label, and nothing else in the pair looks worse.
var tintedRolePairs = []RolePair{
	{Foreground: "--pk-role-fg-brand", Background: "--pk-role-surface-brand-soft", Min: MinContrast},
	{Foreground: "--pk-role-fg-link", Background: "--pk-role-surface-brand-soft", Min: MinContrast},
}

// TintedRolePairs returns the body pairs whose background the semantic layer
// derives by mixing a foreground into a surface. Whoever gates the layer a
// browser paints hands over BodyRolePairs and these: those are what a reader is
// shown on a theme's own surfaces, these what a reader is shown on a surface this
// layer derives from a theme's.
func TintedRolePairs() []RolePair { return slices.Clone(tintedRolePairs) }

// GatedRolePairs returns the whole gated set: every body pair the semantic layer
// composes, on a theme's own surfaces and on the surfaces it derives. One gate
// belongs to one list, so the door that accepts a client's design and the seam
// that turns a pair into a stylesheet read theirs from here rather than each
// assembling its own copy — which is what made the two halves disagree.
func GatedRolePairs() []RolePair {
	return append(BodyRolePairs(), TintedRolePairs()...)
}
