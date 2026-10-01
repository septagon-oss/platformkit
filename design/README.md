# Design themes

`design` owns the values a product changes without touching a component: the
two themes of a `Pair` (colours, typography, shape), their projection as
`--pk-*` tokens, the colour and font-family validation the exports rely on, and
the asset and font-face metadata a design tool records. It imports the
standard library alone — the [package gate](../scripts/check_packages.sh) keeps
it so — and [ui/style](../ui/style/README.md) renders the tokens into the
stylesheet with `style.ThemeVars`.

Start with `design.Default()`. To change a palette, copy `Light()` and `Dark()`,
set the fields you need and pass the `Pair` to `ui.Compose`, `admin.Deps.Theme`
and `web.Deps.Theme`. The utility alphabet, the class lists and the roles they
paint with are declared once and read by everyone, so nothing else changes; the
hand-written rules are the exception and are named for what they are — `base()`
and `componentState()` in [ui.go](../ui/ui.go), and a module's own sheet, reach
for tokens, and `bodyContrast` measures every colour those sheets paint — and
one colour is neither a theme nor a sheet: the tenant's brand colour, which the
site's own page writes over the tokens layer, is asked of `LegibleAccent` before
it is painted. A font stack and a radius are the two paints no contrast list can
read, named in [CHANGELOG](../CHANGELOG.md). `Theme.Typography`
selects the display, body and mono stacks and `Theme.Shape` the button, card and
modal radii; `Theme.Tokens()` is the ordered projection the stylesheet and the
exports share.

## Identity from a seed, and the gate on it

A client that does not want to write hex writes a seed: `design.Seed{Sector,
Name, Brand}` generates both themes in `design.FromSeed`, deterministically, and
refuses unless the pair passes both halves of the gate — `Pair.Check` and
`Pair.CheckRoles` over `RoleLayer()` and `GatedRolePairs()` below. `Check` is
WCAG 2.2 measured by `Luminance` and `Contrast`: every body role at 4.5:1 (SC
1.4.3) — a status tone on every surface a card raises itself onto, not only the
badge that carries its name — and the three graphical paints that carry
information at 3:1 (SC 1.4.11): the focus ring on each of the three body
surfaces its `RingOffset` draws it onto, `border-default`, which is a text
field's only edge — `clInput` fills the field with the same surface role as the
card `clCardFrame` paints, so the line, not the fill, is what says where the
field is — and `border-strong`, the 4 px quotation bar the site's article sheet
gives every `[data-prose] blockquote` on its page canvas; each pair is gated on
the ground a shipped rule draws it on. The sidebar is a fourth ground a ringed
control sits on, ungated, and `surface-active` a fifth ground the layer mixes the
same way as the hover fill and no shipped rule draws a line on; both are named in
[CHANGELOG](../CHANGELOG.md) with their numbers. A
generated foreground is repaired against two grounds no token pair reaches: the
soft brand tint, `SoftTintPercent` of the accent mixed into a surface, which the
kernel paints that accent's own colour on and which reads worse than its source
surface, and the four status tints, which the generator walks toward white (or
black) until the muted line a tinted panel carries reads on them; a generated edge
is repaired against a third such ground, the hovered fill `HoverTintPercent` of
`text-primary` mixes into a surface, because `clButtonVariant["secondary"]` and
`clModalCancel` keep the line while the fill moves onto it. A generated
edge is emitted at 3.5:1, half a step over the floor that gates it, because the
ground a client files is not the ground the generator drew: naming a
`surface-primary` in `design.yaml` moves the card a field stands on, and an edge
emitted at exactly the floor hands the door a refusal of the kernel's own colour
for a change the client made to the surface under it. Repairing the
tint and not the tone is deliberate: `text-muted` is set once per page and is
one of the twelve tokens `Distance` measures. `Distance`, `MinDistance` and
`Colliding` compare two clients' palettes: the generator cannot promise a
separation it does not hold, so a process that would wear both asks and refuses.

[`kit/designconfig`](../kit/designconfig/) decodes `clients/<slug>/design.yaml`
into `design.Client` — the seed plus at most named overrides of the 22 colour
tokens, its own font stacks and its radii in the spelling the tags give them
(`card-radius`) — and `Client.Resolve` runs the same gate over the finished pair:
`Pair.Check` over the 22 tokens, then `Pair.CheckRoles` over the role layer below.
Start there for a new client; `design.Default()` stays what an installation that
says nothing about colour gets.

A colour can also arrive as a row. `site_settings.primary_color` is the one colour
a person types into this application, and `modules/web` pins it on the document as
an unlayered `--pk-color-accent-default`, which outranks the `@layer tokens` block
`ui.Compose` emits and so reaches four gated roles and a badge's tint at paint time.
The write is not the only door that has to hold: a row saved before this gate existed
would still be painted, so the door that paints asks. `Pair.LegibleAccent` takes that
colour and the theme it is going to be painted in and returns the colour the pair can
carry — the one it was given when the finished theme clears both halves of the gate,
otherwise the nearest colour along its own hue that does, measured by the same
`Theme.Check` and `Theme.CheckRoles` a client's file passes through. Where no colour
reads, it answers with an error and the page paints the accent its own stylesheet
carries. Each mode is asked separately, because a colour that reads as a link on a
light card does not read on a dark canvas, and the site declares one measured colour
per mode the page can be in.

## The layer a browser paints

The `--pk-role-*` declarations live in [roles.go](roles.go): every role in terms
of a theme's tokens, the mixes a theme does not enumerate, and the four lists of
pairs a reader is shown — `BodyRolePairs()` on a theme's own surfaces,
`TintedRolePairs()` on the surface the layer derives by mixing a foreground into
one, `StatusRolePairs()` on the four status grounds — tinted, holding a tone
and the muted line a failed upload puts under it, and filled, holding the label of
the button that tone names (`clButtonTone` paints `fg-on-brand` on
`surface-danger` at 14 px, which is what a generated list's delete form wears)
plus `BodyRolePairs()`'s `fg-on-brand` on the accent a primary button is filled
with, and `EdgeRolePairs()` at the graphic's floor over the boundary a component
draws itself with — `border-primary` on the card, the page canvas, the muted panel
and the hovered fill, the four grounds shipped rules draw that line on. They are names, references and percentages, so owning them costs this
package nothing of its dependency rule, and the package that measures a ratio has
to own what it measures: `Client.Resolve` gates a client's finished pair with
`RoleLayer()` and `GatedRolePairs()`, while
[ui/style](../ui/style/README.md) renders those declarations into its `:root`
block and [ui/export](../ui/export/README.md) projects them — all three through
the same one list. A pair this package paints — at body size, or as the edge of the thing the copy
is typed into — that those lists do not
name is a pair no gate measures, which is how a muted sentence inside a
warning-tinted panel sat at 3.6:1 on a generated palette, and how a field's edge
sat at 1.42:1, while every gate in the
repository passed it: `ui/components/painted_status_pair_test.go` now reads the
painted pairs out of the components' own declarations, counts every ground a rule
raises itself onto rather than the ones whose name says `Soft`, and refuses a pair
that is missing from the list. A gate that could read only the token layer could certify
colours nobody paints. One measured instance: a client whose dark theme overrides
`surface-primary` with `#2e2920` holds all 22 token pairs and moves the tint a
brand badge's own label sits on down to 3.91:1 — on the seed that client files,
which is a different accent, tint and surface than any other client's, so the
refusal is a measurement of that pair and not a property of those seven
characters. That is why the door a client's file passes through refuses it rather
than the export alone, and why no literal in this file is a rule.

A role has two true names and this package owns the translation between them.
Declared, it is `fg-muted` — the key of the table above, the spelling
`style.Color` carries and the spelling a component's own class list is written
against — and the three pair lists are written that way, so a reader can compare a
gated name with the rule that paints it. Emitted, it is `--pk-role-fg-muted`, the
property a `:root` block declares, and that is the name `RoleLayer` hands out, the
name a refusal quotes, and the name `ui/style` gives the seam that ships a sheet:
`RoleCSSName` is the one rule, `CheckRoles` resolves either spelling against a
layer and still refuses a name that is neither (`text-muted` included — a token is
not a role), and `ui/style`'s `TestRolePairMirrorsSpellTheEmittedNames` pins the
mirrored lists to this file's, pair for pair, order for order and floor for floor.

Two refusals keep the gate honest rather than theatrical. A colour that carries
alpha is refused before anything is measured: a ratio is a property of two opaque
paints, and premultiplied channels would let `transparent` clear the gate at 21:1.
A font stack is checked by `ParseFontFamilies`, the package's own parser, not by a
looser regexp beside it, and a radius is checked by `Shape.Validate` beside the
type — `px` or `rem`, the units the DTCG document a mobile application reads can
carry.

What a client's identity is made of, in one place (decision 0022). **Reused:**
`Pair`, `Theme`, `Both` and `Tokens` for the value; `colors.go` `parseColor` and
`ResolveColors` for the numbers `Check` reads; `ParseFontFamilies` for the type
grammar; `colorFields` and `shapeFields` for the vocabularies; `ui/export` for the
DTCG document. **Added:** `contrast.go`, because no Go code this repository builds
measured a ratio at all — the vendored editor plug-in under
`tools/designexport/openpencil` ships JavaScript that does, and nothing here
imports it; `seed.go`, because nothing generated a palette; `Client`
and `Distance`/`Colliding`, because nothing read a client's file or compared two
clients' identities; `Shape.Validate`/`ParseRadius`, because the radius grammar
lived in a loader's regexp rather than beside the type; `roles.go`, because the
list that gates a client's override and the list a stylesheet is rendered from
cannot be two lists. **Made reusable:**
`RadiusTokenNames`, `RoleLayer`/`GatedRolePairs` and the exported `Pair.Check`,
`Luminance` and `Contrast` for
any other gate above this package, and `designconfig`'s slug-keyed set as the
place a process holds many clients' identities.

Font assets and their licences are described, not shipped: see
[assets.go](assets.go) and the
[editor bundler](../tools/designexport/openpencil/README.md#deliver-source-backed-font-assets).
`go test ./design ./ui/style` checks the token identities, the colour form, the
font-family syntax and that the rendered stylesheet declares every token.
