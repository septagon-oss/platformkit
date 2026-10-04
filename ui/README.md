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
| [ui/screens](screens/render.go) | The screens of an `httpx.Resource` behind `page.Serve` — up to the seven of a collection, fewer where its operation set withholds a verb, the two of a resource that is not one, and one POST per declared command — and the resource catalog; aliases the renderers. | kit/rest, ui/page, ui/resource. |
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

## The cascade layers

`ui.Compose` returns one sheet in four cascade layers — `tokens`, `base`,
`components`, `client` — and the `@layer` order statement it emits is what says
so; [ARCHITECTURE.md](../ARCHITECTURE.md#compose-the-interface) owns the
precedence contract. What a change to that seam is made of, in the same words a
review reads:

- **Reused** — `components.Hooks`, `css.WalkRules`, `style.For`, `design.Pair`
  and the existing `Extra{Lists, Sheets}` split. `Compose` is still the only
  place a sheet is assembled and `ui/style` still resolves each class list once.
- **Added** — the layer order statement, `componentState` for a kernel
  component's own rules, and `attrNames`/`classNames`/`renderedHooks`/
  `kernelClasses`/`composedClasses`/`css.Heads` for the gate, which reads a
  consumer's rules through `css.Verbatim` and at the heads `css.Heads` names:
  what a browser parses is the emitted text, not the field.
  The gate reads two vocabularies, because a class selector addresses kernel
  markup the same way an attribute selector does and the client layer the gate
  guards is the sheet's strongest layer, not its weakest. A class is addressable
  two ways, and both are read: after a `.` by `classNames`, and as the value of
  the `class` attribute by `attrMatches`, because `[class~="sr-only"]` matches
  the elements `.sr-only` does while a client styles classes with `.name`. The
  class vocabulary is computed per composition: `Extra.Lists` resolve into
  @layer components beside the components' own, and a class that reaches that
  layer by a module's list needs the same refusal as one that reaches it by a
  component.
- **Where the refusals stop** — they are the contract of the sheet a page
  links, and `Compose` is the one place that contract is enforced.
  `ui.ComposeDesign` composes the same four layers in the same order, places
  every rule in the same layer and fingerprints the result the same way, and
  refuses none of it: `ui/export` renders what a proposal asked for — a colour
  with no token yet, a margin on the icon of a component someone is proposing —
  and the captures that measure it have to be able to render a sheet the page
  would refuse. `compose_design_test.go` pins both halves.
- **Made reusable** — `components.Hooks`: the attributes the components render,
  now exported, documented and read by the gate instead of recopied into it,
  beside `components.ClassLists()`, which the class vocabulary is computed from
  rather than copied out of a rendered sheet.

## The frame's floor

The four refusals every client's design gate reported on every generated page
were drawn here, so the cure is here too.

- **Reused** — `style.FgOnInverse`/`style.FgPrimary` chosen by the flavour the
  sidebar already branches on, `style.MaxWScaled(style.MaxWSM)` (the bound
  `document.Bare` and the reference app's fault page already chose),
  `components.Text`'s own size vocabulary, and the gate's own probe as
  `e2e/review-r3-refusal-floor.spec.ts` transcribes it. That bound is carried by
  the two sentences under a control (`clHelp`, `clFieldErr`) and by the wrapper
  inside the footer — never by the field's own flex column (`clFieldWrap`),
  because the design tool projects no composition whose sizing is constrained of
  its own accord: a max-width there took every client's Input, Select, Textarea,
  Checkbox and Form out of their design document — 44 refusals in the tool's own
  suite at the refused head, and 74 more for text that broke mid-word, below.
  An unbounded help line measured
  189ch on every generated record page that documents a field; a paragraph is
  what the floor measures and what the projection carries.
- **Added** — `BreakAnywhere` (`overflow-wrap: anywhere`), which the vocabulary
  lacked and `break-words` is not: it is the value that takes part in intrinsic
  min-content sizing, so a name that is one token stops setting the width of the
  column it sits in. It sits on the frame's content region (`clShellMain`), and
  inherits to every heading, breadcrumb and cell below it, rather than on the
  Heading or Breadcrumb component: the tool builds text only where the element
  itself computes ordinary line breaking, so the rule belongs to the page, which
  is the thing that was scrolling sideways. And one `sm` step for everything the
  frame draws outside `<main>`, because a page is allowed two body sizes and the
  chrome took three.
- **Made reusable** — `e2e/design_floor.ts`: the floor's eight rules, the
  chrome's `<p>` set (header and footer, found from `main`'s parent, no
  data-attribute invented for the shell), the brand link's own contrast and a
  diagnostic that names the element behind a refusal, for every spec written from
  now on; and `ui:"display"`, the field tag by which an entity says what its rows
  are called, read by `ui/resource` and refused at mount by `kit/rest`.

## Next action

To serve a page, compose the stylesheet once with `ui.Compose`, mount
`ui.Assets` beside the API and return views through `page.Serve`; the
[web module](../modules/web/README.md) is the smallest complete shell. To
render the same document or screen without the router — a preview, an export,
a test — call `document.Document` or `resource.List` with values. To add
a component, add its renderer and class lists in `ui/components` and its entry
to `examples.Gallery`; `go test ./ui/components/...` proves every emitted class
resolves to a rule. To change colours or type, supply a `design.Pair`.
