package style

// emission_roles.go renders the semantic colour layer in this package's own
// vocabulary: the Color constants the utility rules are written against, and the
// --pk-role-* block the stylesheet declares. The layer itself — every role's
// source value in terms of a theme's tokens, and the body pairs a reader is
// shown — is declared by package design (design/roles.go), which is the package
// that measures a ratio: the list that gates a client's finished pair and the
// list a stylesheet is built from are then the same list, read from one place.
// Retheming therefore never touches the utility rules — a different theme
// changes the values behind the same roles.

import (
	"strings"

	"github.com/septagon-oss/platformkit/design"
)

// roleValues projects design's declarations into the Color vocabulary this
// package compiles rules for, keyed by role. TestRoleMapCoversEveryColor pins
// the projection to AllColors(), so a Color with no declared value, or a
// declared role no rule compiles, fails this package's tests.
func roleValues() map[Color]design.ColorValue {
	declarations := design.RoleLayer()
	out := make(map[Color]design.ColorValue, len(declarations))
	for _, role := range declarations {
		out[Color(strings.TrimPrefix(role.Name, "--pk-role-"))] = role.Value
	}
	return out
}

// RoleColors returns the layer RoleVars renders: fresh, ordered declarations of
// every role in terms of theme token variables. Resolve them with one selected
// Theme.Tokens() using design.ResolveColors; no mode, palette or native support
// is inferred here.
func RoleColors() []design.ColorToken { return design.RoleLayer() }

// BodyRolePairs returns the body-size text pairs this package's roles compose on
// a theme's own surfaces, for design.Theme.CheckRoles. A caller that emits a
// stylesheet hands these over with RoleColors: the token gate says what a theme
// sets, these say what a reader is shown. design declares them in the layer's own
// vocabulary; these are the same pairs spelled in the names this package emits,
// because the sheet a caller gates is written in them — and
// TestRolePairMirrorsSpellTheEmittedNames pins that the two lists are one list.
func BodyRolePairs() []design.RolePair { return emittedRolePairs(design.BodyRolePairs()) }

// emittedRolePairs re-spells design's pair lists in the names this package
// renders: the same pairs, the same floors, each role under the --pk-role-
// property a browser paints. The translation is design's own, so no second copy
// of the naming rule lives here.
func emittedRolePairs(pairs []design.RolePair) []design.RolePair {
	out := make([]design.RolePair, len(pairs))
	for i, pair := range pairs {
		pair.Foreground, pair.Background = design.RoleCSSName(pair.Foreground), design.RoleCSSName(pair.Background)
		out[i] = pair
	}
	return out
}

// TintedRolePairs returns the body pairs whose background the layer derives by
// mixing a foreground into a surface — design.SoftTintPercent of the accent into
// the card surface, which a brand badge, the active navigation link and an
// outline button's hover state paint the accent's own colour on. design
// certifies a generated accent against that tint, and design.Client.Resolve
// gates a client's override against it, so a pair that reaches this package
// already holds it; a caller that gates what it ships gates both lists anyway.
func TintedRolePairs() []design.RolePair { return emittedRolePairs(design.TintedRolePairs()) }

// GatedRolePairs is the whole gated set: BodyRolePairs, TintedRolePairs and
// StatusRolePairs. The
// seam where a Pair becomes a stylesheet, an export or a Storybook hands these
// to design.Pair.CheckRoles with RoleColors, which is the same pair of arguments
// design.Client.Resolve reads — the two halves of one gate.
func GatedRolePairs() []design.RolePair { return emittedRolePairs(design.GatedRolePairs()) }

// StatusRolePairs returns the body pairs a reader is shown on a status ground:
// each tone on its own badge, the muted line a tinted panel carries — clMediaFailed
// gives a failed media panel the warning ground and fills it with the muted reason
// line, so the tint is certified against both — and the label of the button a tone
// names outright, which clButtonTone paints as fg-on-brand on the tone itself at
// text-sm. design certifies a generated tint against that muted tone; the fill
// needs no repair, because the label is the neutral pole every fill is walked away
// from, and design.Client.Resolve gates a client's override against every one of
// these pairs, so a pair that reaches this package already holds it.
func StatusRolePairs() []design.RolePair { return emittedRolePairs(design.StatusRolePairs()) }

// roleVar renders the var() reference utility rules use for a color role.
func roleVar(c Color) string { return "var(--pk-role-" + string(c) + ")" }
