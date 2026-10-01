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
//
// One list, read in two vocabularies, both owned here, because a role has two
// true names. Declared, it is "fg-muted": the key of roleLayer below, the
// spelling ui/style's Color constants carry, the name a class list is written
// against. Emitted, it is "--pk-role-fg-muted": the name RoleLayer hands out, the
// property the :root block declares and a browser paints. RoleCSSName turns one
// into the other and CheckRoles resolves either against a layer, so the pair
// lists below are written in the declared spelling — the vocabulary this file
// declares the layer in — while ui/style, the door that ships a stylesheet, hands
// its seam the emitted spelling. Either way a refusal names the property a person
// would find in the sheet, and a pair read out of a component's own declaration
// names the role the same file writes.

import (
	"maps"
	"slices"
	"strings"
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
// SurfaceActive is the one declared paint no kernel rule composes yet: declared
// because the export ships it to a native application, and gated nowhere only
// because nothing in this repository paints copy on the ground. The phone can.
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
	"surface-hover":        roleMix(roleRef("text-primary"), HoverTintPercent, roleRef("surface-primary")),
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
		out = append(out, ColorToken{Name: rolePrefix + name, Value: detach(roleLayer[name])})
	}
	return out
}

// RoleCSSName returns the name a role carries in a resolved layer: the emitted
// spelling, with the --pk-role- prefix a stylesheet declares and a browser reads.
// A name already in that spelling is returned as it stands, so the function is a
// translation into one vocabulary rather than a concatenation, and it is what
// makes a pair written as "fg-muted" and a pair written as "--pk-role-fg-muted"
// the same measurement. An empty name is not a role: it comes back as a name no
// layer declares, so a caller that lost one is refused rather than skipped.
func RoleCSSName(name string) string {
	if strings.HasPrefix(name, rolePrefix) {
		return name
	}
	return rolePrefix + name
}

// rolePrefix is the property prefix the renderers emit for a role.
const rolePrefix = "--pk-role-"

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
	{Foreground: "fg-primary", Background: "surface-primary", Min: MinContrast},
	{Foreground: "fg-primary", Background: "surface-secondary", Min: MinContrast},
	{Foreground: "fg-primary", Background: "surface-tertiary", Min: MinContrast},
	{Foreground: "fg-on-surface", Background: "surface-primary", Min: MinContrast},
	{Foreground: "fg-on-surface", Background: "surface-tertiary", Min: MinContrast},
	{Foreground: "fg-secondary", Background: "surface-primary", Min: MinContrast},
	{Foreground: "fg-secondary", Background: "surface-secondary", Min: MinContrast},
	{Foreground: "fg-secondary", Background: "surface-tertiary", Min: MinContrast},
	{Foreground: "fg-tertiary", Background: "surface-primary", Min: MinContrast},
	{Foreground: "fg-tertiary", Background: "surface-secondary", Min: MinContrast},
	{Foreground: "fg-tertiary", Background: "surface-tertiary", Min: MinContrast},
	{Foreground: "fg-muted", Background: "surface-primary", Min: MinContrast},
	{Foreground: "fg-muted", Background: "surface-secondary", Min: MinContrast},
	{Foreground: "fg-muted", Background: "surface-tertiary", Min: MinContrast},
	{Foreground: "fg-placeholder", Background: "surface-primary", Min: MinContrast},
	{Foreground: "fg-placeholder", Background: "surface-secondary", Min: MinContrast},
	{Foreground: "fg-placeholder", Background: "surface-tertiary", Min: MinContrast},
	// The accent as copy a reader reads: the brand text utility, a brand detail
	// value and the outline button variant paint fg-brand with no background of
	// their own, a link paints fg-link (the same colour, in a sentence) and its
	// hover state paints fg-link-hover, so all three land on whichever surface
	// the card around them raised itself onto.
	{Foreground: "fg-brand", Background: "surface-primary", Min: MinContrast},
	{Foreground: "fg-brand", Background: "surface-secondary", Min: MinContrast},
	{Foreground: "fg-brand", Background: "surface-tertiary", Min: MinContrast},
	{Foreground: "fg-link", Background: "surface-primary", Min: MinContrast},
	{Foreground: "fg-link", Background: "surface-secondary", Min: MinContrast},
	{Foreground: "fg-link", Background: "surface-tertiary", Min: MinContrast},
	{Foreground: "fg-link-hover", Background: "surface-primary", Min: MinContrast},
	{Foreground: "fg-link-hover", Background: "surface-secondary", Min: MinContrast},
	{Foreground: "fg-link-hover", Background: "surface-tertiary", Min: MinContrast},
	{Foreground: "fg-on-inverse", Background: "surface-inverse", Min: MinContrast},
	// The label of a control the theme fills with its own colour: the primary
	// button variant, the brand tone, the current pagination item and the active
	// pill tab paint fg-on-brand on surface-brand = the accent itself. The token
	// gate already measures accent-on against accent-default; this is the same
	// ratio spelled in the vocabulary a stylesheet paints, which is what lets one
	// sweep of the painted rules cover a button and a tint together.
	{Foreground: "fg-on-brand", Background: "surface-brand", Min: MinContrast},
}

// BodyRolePairs returns the body-size text pairs the semantic layer composes on
// a theme's own surfaces, for Theme.CheckRoles. Whoever gates a palette hands
// these over with RoleLayer: the token gate says what a theme sets, these say
// what a reader is shown. Roles are named as this file declares them, without the
// --pk-role- prefix a stylesheet emits; RoleCSSName spells them the other way.
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
	{Foreground: "fg-brand", Background: "surface-brand-soft", Min: MinContrast},
	{Foreground: "fg-link", Background: "surface-brand-soft", Min: MinContrast},
}

// TintedRolePairs returns the body pairs whose background the semantic layer
// derives by mixing a foreground into a surface. Whoever gates the layer a
// browser paints hands over BodyRolePairs and these: those are what a reader is
// shown on a theme's own surfaces, these what a reader is shown on a surface this
// layer derives from a theme's.
func TintedRolePairs() []RolePair { return slices.Clone(tintedRolePairs) }

// statusRolePairs names the body pairs painted on a status ground, which is
// neither a theme's plain surface nor a surface this layer mixes: it is the
// fourth ground a component raises itself onto, and it comes in two strengths.
// The soft tint is the badge and the panel — clBadgeTone and clAlertVariant paint
// each tone on its own soft ground at text-sm, and a tint carries copy that is
// not a status tone at all: clMediaFailed gives a media panel the warning ground
// and mediaAbsent fills that panel with clEmptyDesc, the muted reason line, at
// 14 px. The full fill is the button — clButtonTone replaces a button's whole
// appearance with the tone itself, Bg(style.SurfaceDanger) under
// TextColor(style.FgOnBrand) at text-sm, and ui/resource asks for Tone: "danger"
// on the delete form of every generated list. The token gate measures a tone
// against the badge named after it (status-warning against status-warningbg) and
// accent-on against the accent, and says nothing about either line of copy these
// two grounds carry, so an override of text-muted, of a tint, of a fill or of
// accent-on moved words nobody measured: these are the pairs Client.Resolve was
// blind to.
//
// The muted tone is gated against all four tints rather than the one a panel
// wears today, and the label against all four fills rather than the one a delete
// form wears today, because the pair is one merge deep on any of them —
// clMediaPanel picks the ground and clEmptyDesc picks the colour, clButtonTone
// picks both and an alert already takes arbitrary nodes in its action slot — and
// a rule a reader can state in one sentence ("a body foreground on a status
// ground, tinted or filled, is a measured pair") holds better than one that names
// a component. What is not here stays out: fg-link and fg-disabled on a tint
// (round 7's deferred finding, which belongs to the change that paints it). The
// plain surfaces are bodyRolePairs' own sweep, and it names every foreground the
// kernel paints on them, fg-on-brand on the brand fill included — a ground being
// "plain" was never a reason for a foreground to be unmeasured.
var statusRolePairs = []RolePair{
	{Foreground: "fg-success", Background: "surface-success-soft", Min: MinContrast},
	{Foreground: "fg-warning", Background: "surface-warning-soft", Min: MinContrast},
	{Foreground: "fg-danger", Background: "surface-danger-soft", Min: MinContrast},
	{Foreground: "fg-info", Background: "surface-info-soft", Min: MinContrast},
	{Foreground: "fg-muted", Background: "surface-success-soft", Min: MinContrast},
	{Foreground: "fg-muted", Background: "surface-warning-soft", Min: MinContrast},
	{Foreground: "fg-muted", Background: "surface-danger-soft", Min: MinContrast},
	{Foreground: "fg-muted", Background: "surface-info-soft", Min: MinContrast},
	// The same ground at full strength, with the button's label on it.
	{Foreground: "fg-on-brand", Background: "surface-success", Min: MinContrast},
	{Foreground: "fg-on-brand", Background: "surface-warning", Min: MinContrast},
	{Foreground: "fg-on-brand", Background: "surface-danger", Min: MinContrast},
	{Foreground: "fg-on-brand", Background: "surface-info", Min: MinContrast},
}

// StatusRolePairs returns the body pairs a reader is shown on a status ground:
// the tone on its own badge, the muted copy a tinted panel carries, and the label
// a filled status button paints. Whoever gates the layer a browser paints hands
// over BodyRolePairs, TintedRolePairs, StatusRolePairs and EdgeRolePairs.
func StatusRolePairs() []RolePair { return slices.Clone(statusRolePairs) }

// edgeRolePairs names the pairs whose job is to be seen and not read: the line a
// component draws itself with, on the ground it is drawn on. A text field decides
// it. clInput (ui/components/classlists.go) fills the field with
// style.SurfacePrimary — the same role clCardFrame paints the card with — so the
// fill carries nothing about where the field is, and clInputNormal's
// BorderColor(style.BorderPrimary) is the only visual information identifying the
// component and its extent. SC 1.4.11 asks 3:1 of exactly that (its own worked
// example is a text input's border), which is more than a decorative divider is
// asked for and less than a sentence, so the floor is MinContrastGraphic: the one
// this layer already holds the focus ring to.
//
// The grounds here are the page's own: the surfaces a card raises itself onto, read
// out of the shipped rules rather than guessed — and a case re-runs that claim, for
// review 16 found it false twice. review_round16_edge_ground_coverage_test.go reads
// the rule declarations back against this list in both directions, so a ground named
// here that nothing paints and a ground painted here that nothing names both fail
// that case. They are the card (surface-primary) and the page canvas
// (surface-secondary) a field stands on (clInput/clInputNormal, clCardFrame — round
// 14); the muted panel (surface-tertiary) a neutral alert and a refused media panel
// draw their border on (clAlertVariant["neutral"], clMediaRefused); and the control's
// own hovered fill (surface-hover), because clButtonVariant["secondary"] and
// clModalCancel keep painting BorderColor(style.BorderPrimary) while
// hoverBg(style.SurfaceHover) moves the fill underneath it — a ground the layer mixes
// at HoverTintPercent of text-primary over surface-primary rather than a token a
// client files, which is why that pair belongs to this list and not to the token half
// in contrast.go. surface-active is mixed the same way as surface-hover and joins no
// list: no shipped rule draws a line on it (grep style.SurfaceActive over ui/,
// modules/ and apps/ answers only a test), and an unpainted pair has no business in
// the list a client's file is refused by. A line drawn across a panel the theme fills
// with a colour of its own is measured too, and belongs to PanelEdgeRolePairs: the
// two grounds give way to a line in opposite directions, and the sentence saying so is
// written there.
//
// What stays exempt is the decoration, not the boundary: a divider drawn between
// two rows the reader has already found, and a tint, whose copy the body and
// tinted pairs measure themselves. border-secondary is border-default walked 60 %
// toward surface-primary and is therefore lighter than the edge gated here by
// construction; it is the exempt class — a line inside something the reader has
// already found — and it is named here rather than left to be inferred.
var edgeRolePairs = []RolePair{
	{Foreground: "border-primary", Background: "surface-primary", Min: MinContrastGraphic},
	{Foreground: "border-primary", Background: "surface-secondary", Min: MinContrastGraphic},
	{Foreground: "border-primary", Background: "surface-tertiary", Min: MinContrastGraphic},
	{Foreground: "border-primary", Background: "surface-hover", Min: MinContrastGraphic},
}

// EdgeRolePairs returns the pairs whose role is to be seen rather than read: a
// component's own edge on each ground it is drawn on, at MinContrastGraphic. A
// gate that measures copy and a ring but not the boundary of the field the copy is
// typed into certifies a page whose inputs have no visible edges.
func EdgeRolePairs() []RolePair { return slices.Clone(edgeRolePairs) }

// panelEdgeRolePairs names the boundary a component draws across a panel the theme
// fills with a colour of its own, rather than on a surface a card raises itself onto.
// One pair ships today: the rule under a sidebar section label.
// clSidebarSectionHeader (ui/components/classlists.go) draws BorderColor(style.BorderPrimary)
// and declares no fill of its own, and clSidebarColumnAdmin fills the column it sits
// inside with Bg(style.SurfaceInverse) — the sidebar-bg token, which a seed decides
// and a client may file. ui/components/sidebar.go applies that pair to every sidebar
// that carries a labelled section and is not the content flavour, the storybook's own
// sidebar example included, and round 21 measured a palette the generator issues at
// 2.65:1 there. The line is what says where the labelled section ends, so it is a
// boundary and not a divider, and SC 1.4.11 asks 3:1 of it.
//
// This is a separate list from edgeRolePairs because the two grounds give way to a
// line in opposite directions, and because merging them would state something false.
// The page grounds are light in a light theme, so the line is repaired dark against
// them and the surfaces — literals a client files — stay where they were filed. The
// panel ground is dark in a light theme, because it carries the theme's own light
// copy: that is what a sidebar is. A line dark enough for the page disappears on it,
// so the panel is the half with room, and generatedTheme darkens the panel under the
// line for that reason. Merged into edgeRolePairs, the pair would also be read as a
// claim about every rule that declares no fill of its own — the reading review 16's
// coverage case runs — and that claim is both false and unsatisfiable here:
// clInputError, clInputTone and clTabsUnderlineActive draw a status tone or the brand
// colour with no fill of their own on the page's surfaces and never inside a sidebar,
// and a status tone held to MinContrast on a light card (luminance at most .17)
// cannot reach 3:1 on a panel darker than luminance .60. Naming that pair in the
// shared list would ask for a danger red that reads on both, which is a colour that
// does not exist.
var panelEdgeRolePairs = []RolePair{
	{Foreground: "border-primary", Background: "surface-inverse", Min: MinContrastGraphic},
}

// PanelEdgeRolePairs returns the boundaries drawn on a panel the theme fills with its
// own colour, at MinContrastGraphic. Whoever gates a palette hands these over beside
// EdgeRolePairs, as it does TintedRolePairs beside BodyRolePairs: one gate, and the
// two lists because the two grounds give way in opposite directions.
func PanelEdgeRolePairs() []RolePair { return slices.Clone(panelEdgeRolePairs) }

// GatedRolePairs returns the whole gated set: every body pair the semantic layer
// composes, on a theme's own surfaces, on the surfaces it derives by mixing, on
// the status tints, the edge a component draws itself with, and the boundary it
// draws across a panel the theme fills with its own colour. One gate belongs to one
// list, so the door that accepts a client's design and the seam that turns a pair
// into a stylesheet read theirs from here rather than each assembling its own copy —
// which is what made the two halves disagree. The list is a union of five because
// each kind of ground gives way to its pair in its own direction; the seams that read
// one kind alone (review 16's coverage case, for one) read the smaller list.
func GatedRolePairs() []RolePair {
	pairs := BodyRolePairs()
	pairs = append(pairs, TintedRolePairs()...)
	pairs = append(pairs, StatusRolePairs()...)
	pairs = append(pairs, EdgeRolePairs()...)
	return append(pairs, PanelEdgeRolePairs()...)
}
