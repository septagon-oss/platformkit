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
refuses the result unless `Pair.Check` passes. `Check` is WCAG 2.2 measured by
`Luminance` and `Contrast`: every body role at 4.5:1 (SC 1.4.3), the focus ring
at 3:1 (SC 1.4.11). `Distance`, `MinDistance` and `Colliding` are how two
clients' palettes are compared — the generator cannot promise a separation it
does not hold, so a process that would wear both asks and refuses.

[`kit/designconfig`](../kit/designconfig/) decodes `clients/<slug>/design.yaml`
into `design.Client` — the seed plus at most named overrides of the 22 colour
tokens, its own font stacks and its radii in the spelling the tags give them
(`card-radius`) — and `Client.Resolve` runs the same gate over the finished pair.
Start there for a new client; `design.Default()` stays what an installation that
says nothing about colour gets.

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
DTCG document. **Added:** `contrast.go`, because no Go code in this workspace
measured a ratio at all; `seed.go`, because nothing generated a palette; `Client`
and `Distance`/`Colliding`, because nothing read a client's file or compared two
clients' identities; `Shape.Validate`/`ParseRadius`, because the radius grammar
lived in a loader's regexp rather than beside the type. **Made reusable:**
`RadiusTokenNames` and the exported `Pair.Check`, `Luminance` and `Contrast` for
any other gate above this package, and `designconfig`'s slug-keyed set as the
place a process holds many clients' identities.

Font assets and their licences are described, not shipped: see
[assets.go](assets.go) and the
[editor bundler](../tools/designexport/openpencil/README.md#deliver-source-backed-font-assets).
`go test ./design ./ui/style` checks the token identities, the colour form, the
font-family syntax and that the rendered stylesheet declares every token.
