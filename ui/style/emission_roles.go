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
// sets, these say what a reader is shown.
func BodyRolePairs() []design.RolePair { return design.BodyRolePairs() }

// TintedRolePairs returns the body pairs whose background the layer derives by
// mixing a foreground into a surface — design.SoftTintPercent of the accent into
// the card surface, which a brand badge, the active navigation link and an
// outline button's hover state paint the accent's own colour on. design
// certifies a generated accent against that tint, and design.Client.Resolve
// gates a client's override against it, so a pair that reaches this package
// already holds it; a caller that gates what it ships gates both lists anyway.
func TintedRolePairs() []design.RolePair { return design.TintedRolePairs() }

// GatedRolePairs is the whole gated set: BodyRolePairs and TintedRolePairs. The
// seam where a Pair becomes a stylesheet, an export or a Storybook hands these
// to design.Pair.CheckRoles with RoleColors, which is the same pair of arguments
// design.Client.Resolve reads — the two halves of one gate.
func GatedRolePairs() []design.RolePair { return design.GatedRolePairs() }

// roleVar renders the var() reference utility rules use for a color role.
func roleVar(c Color) string { return "var(--pk-role-" + string(c) + ")" }
