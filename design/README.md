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

Font assets and their licences are described, not shipped: see
[assets.go](assets.go) and the
[editor bundler](../tools/designexport/openpencil/README.md#deliver-source-backed-font-assets).
`go test ./design ./ui/style` checks the token identities, the colour form, the
font-family syntax and that the rendered stylesheet declares every token.
