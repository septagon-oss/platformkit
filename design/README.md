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
and `web.Deps.Theme`; every rule above the tokens is written in terms of a
role, so nothing else changes. `Theme.Typography` selects the display, body and
mono stacks and `Theme.Shape` the button, card and modal radii; `Theme.Tokens()`
is the ordered projection the stylesheet and the exports share.

## Identity from a seed, and the gate on it

A client that does not want to write hex writes a seed: `design.Seed{Sector,
Name, Brand}` generates both themes in `design.FromSeed`, deterministically, and
refuses the result unless the pair passes both halves of the gate — `Pair.Check`
and `Pair.CheckRoles` over `RoleLayer()` and `GatedRolePairs()` below. `Check` is
WCAG 2.2 measured by `Luminance` and `Contrast`: every body role at 4.5:1 (SC
1.4.3) — a status tone on every surface a card raises itself onto, not only the
badge that carries its name — and the focus ring at 3:1 (SC 1.4.11). A generated
foreground is repaired against two grounds no token pair reaches: the soft brand
tint, `SoftTintPercent` of the accent mixed into a surface, which the kernel
paints that same accent's colour on and which always reads worse than the surface
the mix was taken from, and the four status tints, which the generator walks
toward white (or black) until the muted line a tinted panel carries reads on
them. Repairing the tint rather than the tone is deliberate: `text-muted` is set
once per page and is one of the twelve tokens `Distance` measures.
`Distance`, `MinDistance` and `Colliding` are how two clients' palettes are
compared — the generator cannot promise a separation it does not hold, so a
process that would wear both asks and refuses.

[`kit/designconfig`](../kit/designconfig/) decodes `clients/<slug>/design.yaml`
into `design.Client` — the seed plus at most named overrides of the 22 colour
tokens, its own font stacks and its radii in the spelling the tags give them
(`card-radius`) — and `Client.Resolve` runs the same gate over the finished pair:
`Pair.Check` over the 22 tokens, then `Pair.CheckRoles` over the role layer below.
Start there for a new client; `design.Default()` stays what an installation that
says nothing about colour gets.

## The layer a browser paints

The `--pk-role-*` declarations live in [roles.go](roles.go): every role in terms
of a theme's tokens, the mixes a theme does not enumerate, and the three lists of
pairs a reader is shown — `BodyRolePairs()` on a theme's own surfaces,
`TintedRolePairs()` on the surface the layer derives by mixing a foreground into
one, and `StatusRolePairs()` on the four status tints, which hold a status tone
and, in the panel a failed upload raises, the muted line under it. They are names,
references and percentages, so owning them costs this
package nothing of its dependency rule, and the package that measures a ratio has
to own what it measures: `Client.Resolve` gates a client's finished pair with
`RoleLayer()` and `GatedRolePairs()`, while
[ui/style](../ui/style/README.md) renders those declarations into its `:root`
block and [ui/export](../ui/export/README.md) projects them — all three through
the same one list. A pair this package paints at body size and that list does not
name is a pair no gate measures, which is how a muted sentence inside a
warning-tinted panel sat at 3.6:1 on a generated palette while every gate in the
repository passed it: `ui/components/painted_status_pair_test.go` now reads the
painted pairs out of the components' own declarations and refuses one that is
missing from the list. A gate that could read only the token layer could certify
colours nobody paints: overriding `surface-primary` with `#2e2920` in a dark theme
holds all 22 token pairs and moves the tint a brand badge's own label sits on down
to 3.91:1, which is why the door a client's file passes through refuses it rather
than the export alone.

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
