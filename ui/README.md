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
| [ui/document](document/document.go) | Chrome, a plain Request, View, `Document`, `Render`, the recovery notices: an HTML document as values. | kit/locale, ui, ui/components/examples. |
| [ui/resource](resource/resource.go) | List, detail and form screens rendered from an entity schema and plain rows. | kit/entity, kit/entity/display, ui/forms, ui/document. |
| [ui/page](page/README.md) | The typed Request, `Serve`, `Render` into an `httpx.Page`, navigation, the locale seam; aliases the document types. | kit/httpx, ui/document. |
| [ui/screens](screens/render.go) | The screens of an `httpx.Resource` behind `page.Serve` — the seven of a collection, the two of a resource that is not one, and one POST per declared command — and the resource catalog; aliases the renderers. | kit/rest, ui/page, ui/resource. |
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
- `ui/document` adds kit/locale and `ui` to that list; `ui/resource` adds
  kit/entity/display, ui/forms and ui/document. Neither reaches kit/db,
  net/http or a module: a document is values, and a screen is a schema plus
  the rows a caller read.
- `ui/page` and `ui/screens` are the adapters and are gated at the adapter
  closure, which reaches kit/db and net/http through kit/httpx; neither may
  import `ui/export` or `ui/source`. What needs the kernel stays there: the
  local-path check on a sign-in link, the nonce on an inline script, the
  guarded closures of an `httpx.Resource`.
- `ui/components` never imports `ui/components/examples`: a renderer needs no
  reflection. `ui` never imports `ui/export`: a shell needs no design tooling.

## Next action

To serve a page, compose the stylesheet once with `ui.Compose`, mount
`ui.Assets` beside the API and return views through `page.Serve`; the
[web module](../modules/web/README.md) is the smallest complete shell. To
render the same document or screen without the router — a preview, an export,
a test — call `document.Document` or `resource.List` with values. To add
a component, add its renderer and class lists in `ui/components` and its entry
to `examples.Gallery`; `go test ./ui/components/...` proves every emitted class
resolves to a rule. To change colours or type, supply a `design.Pair`.
