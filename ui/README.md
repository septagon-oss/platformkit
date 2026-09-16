# Presentation packages

`ui/` is the server-rendered interface: Go functions that return HTML and a
stylesheet that is a Go value. [ARCHITECTURE.md](../ARCHITECTURE.md#compose-the-interface)
explains the boundaries; this page maps the packages and the rules the gates enforce.

| Package | Owns | Depends on |
|---|---|---|
| [design](../design/README.md) | Theme values: colours, typography, shape, tokens. | Standard library only. |
| [ui/css](css/css.go) | The stylesheet intermediate representation. | Standard library only. |
| [ui/icon](icon/icon.go) | The vendored glyph set. | Standard library only. |
| [ui/style](style/README.md) | Class lists, their rules, role and theme variables. | design, ui/css. |
| [ui/components](components/README.md) | Typed renderers: atoms, molecules, sections, layouts, shell. | ui/style, ui/icon. |
| [ui/components/examples](components/examples/example.go) | Captured invocations, their descriptions and the Gallery. | ui/components; the reflection lives here, not in the renderers. |
| [ui](ui.go) | `Compose`, `Sheet`, `Controllers`, `Assets`: what a shell serves. | design, ui/style, ui/components. |
| [ui/forms](forms/README.md) | Portable entity forms without a router or a database. | kit/entity, ui/components, ui/components/examples. |
| [ui/page](page/README.md) | Chrome, Request, View, `Serve`, `Render`, the locale seam. | kit/httpx, kit/locale, ui. |
| [ui/screens](screens/render.go) | List, detail and form screens generated from a resource schema. | kit/rest, ui/forms, ui/page. |
| [ui/export](export/README.md) | Snapshots, DTCG tokens, proposals, the Storybook composition. | ui, ui/components/examples. |
| [ui/source](source/source.go) | Development-time persistence of proposals into Go source. | ui/export, go/packages, dst. |
| [ui/storybook](storybook/README.md) | The optional Storybook.js build (Node). | An `export.Export` snapshot. |
| [ui/assets/js](assets/js/) | htmx and the browser controllers `ui.Controllers` lists. | — |

## Dependency rules

[scripts/check_packages.sh](../scripts/check_packages.sh) runs in `make check`
and refuses growth beyond these recorded closures:

- `design` imports the standard library alone; `ui/style` renders its tokens.
- `ui/forms` reaches kit/entity, design, ui/css, ui/style, ui/icon,
  ui/components, ui/components/examples and gomponents, nothing else.
- `ui/page` and `ui/screens` are gated at their current closure, which reaches
  kit/db and net/http through kit/httpx; neither may import `ui/export` or
  `ui/source`. Freeing them from the database is a separate change.
- `ui/components` never imports `ui/components/examples`: a renderer needs no
  reflection. `ui` never imports `ui/export`: a shell needs no design tooling.

## Next action

To serve a page, compose the stylesheet once with `ui.Compose`, mount
`ui.Assets` beside the API and return views through `page.Serve`; the
[web module](../modules/web/README.md) is the smallest complete shell. To add
a component, add its renderer and class lists in `ui/components` and its entry
to `examples.Gallery`; `go test ./ui/components/...` proves every emitted class
resolves to a rule. To change colours or type, supply a `design.Pair`.
