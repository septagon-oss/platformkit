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
| [ui/resource](resource/README.md) | List, detail and form screens rendered from resolved policy and plain rows. | kit/entity, kit/entity/display, kit/entity/present, ui/forms, ui/document. |
| [ui/page](page/README.md) | The typed Request, `Serve`, `Render` into an `httpx.Page`, navigation, the locale seam; aliases the document types. | kit/httpx, ui/document. |
| [ui/screens](screens/render.go) | The screens of an `httpx.Resource` behind `page.Serve` — up to the seven of a collection, fewer where its operation set withholds a verb, the two of a resource that is not one, and one POST per declared command — and the resource catalog; aliases the renderers. | kit/rest, ui/page, ui/resource. |
| [ui/export](export/README.md) | Snapshots, DTCG tokens, proposals, the Storybook composition. | ui, ui/components/examples. |
| [ui/source](source/source.go) | Development-time persistence of proposals into Go source. | ui/export, go/packages, dst. |
| [ui/storybook](storybook/README.md) | The optional Storybook.js build (Node). | An `export.Export` snapshot. |
| [ui/assets/js](assets/js/) | htmx and the browser controllers `ui.Controllers` lists. | — |

## Composition

**Reused** — `style.For`, component renderers and captures, `forms.Control`,
`resource.List`/`FormExample`, `page.Serve` and `screens.Mount` remain the shared
presentation path. **Added** — private rendering, validation and projection steps
within those packages, because the existing owners already carry the capability.
**Made reusable** — focused steps for controls, grouped tables, token projection
and request rendering, with the same public contracts and output.

Class compilation is organized by family; component Props and class-list
inventories have separate source files. Resource [lists](resource/list.go),
[details](resource/detail.go) and [forms](resource/form.go) share the existing
[resource contract](resource/resource.go). The package boundaries above stay the
same, so consumers keep their imports and constructors.

## Dependency rules

[scripts/check_packages.sh](../scripts/check_packages.sh) runs in `make check`
and refuses growth beyond these recorded closures:

- `design` imports the standard library alone; `ui/style` renders its tokens.
- `ui/forms` reaches kit/entity, design, ui/css, ui/style, ui/icon,
  ui/components, ui/components/examples and gomponents, nothing else.
- `ui/document` adds kit/locale and `ui` to that list; `ui/resource` adds
  kit/entity/display, kit/entity/present, ui/forms and ui/document. Neither reaches kit/db,
  net/http or a module: a document is values, and a screen is a schema plus
  the rows a caller read.
- `ui/page` and `ui/screens` are the adapters and are gated at the adapter
  closure, which reaches kit/db and net/http through kit/httpx; neither may
  import `ui/export` or `ui/source`. What needs the kernel stays there: the
  local-path check on a sign-in link, the nonce on an inline script, a view's
  `RunsWASM` declaration (the kernel's `httpx.AllowWASM`, called for the rendered
  view and for no refusal or redirect), the guarded closures of an
  `httpx.Resource`.
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

### Client-sheet admission

[The admission check](client_sheet.go) reads emitted CSS, including keyframe
stops, so a declaration cannot hide syntax in its property or value. It refuses
consumer layers, block/comment delimiters, HTML style terminators, leading
at-rules and unquoted semicolons in rule heads. CSS names are decoded before
checking protected attributes, component-layer classes, `:root`, literal colours
and `--pk-` declarations. Variable references remain available to client rules.
Closed strings and URL arguments are masked for colour checks; spaces preserve
token separation. An escaped `@` remains an identifier, not an at-rule token.

The policy checks names, including class-attribute comparisons, wherever they
occur in a selector. It does not implement selector reachability. Type and ID
selectors can still reach kernel elements; the product owns the CSS it supplies.
Named colour words also need author review. Violations panic at composition,
before the refused sheet supplies bytes. `ComposeDesign` uses the same placement
and fingerprinting while admitting proposals that need measurement.

### Browser assets

[Assets](assets.go) overlays computed stylesheets, consumer trees and embedded
browser files in that order. `Controllers` remains declared in [ui.go](ui.go).
Calendar/map engines, live-region SSE and the rich-text editor load only when
needed. The editor bundle lives under `assets/richtext`, with its licenses and
locked build under `tools/richtext-bundle`; a directory named `vendor` would be
omitted from Go module archives and break consumers' embedded assets.
Design snapshots and Storybook composition remain in [ui/export](export/README.md).

## The frame's floor

The four refusals every client's design gate reported on every generated page
were drawn here, so the cure is here too.

- **Reused** — `style.FgOnInverse`/`style.FgPrimary` chosen by the flavour the
  sidebar already branches on, `style.MaxWScaled(style.MaxWSM)` (the bound
  `document.Bare` and the reference app's fault page already chose),
  `components.Text`'s own size vocabulary, and the gate's own probe as
  `e2e/refusal-floor.spec.ts` transcribes it. That bound is carried by
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
  is the thing that was scrolling sideways. `BreakNormal` is its opt-out, and one
  region takes it: the table. A cell that broke anywhere let the longest token in
  any column set how narrow every other column became — on a generated list at
  320px the row link's target fell to an 18px band, under the 24px the
  accessibility audit enforces — and a table too wide for the page is already
  reached by scrolling its own named region. And one `sm` step for everything the
  frame draws outside `<main>`, because a page is allowed two body sizes and the
  chrome took three.
- **Made reusable** — `e2e/design_floor.ts`: the floor's eight rules, the
  chrome's `<p>` set (header and footer, found from `main`'s parent, no
  data-attribute invented for the shell), the brand link's own contrast and a
  diagnostic that names the element behind a refusal, for every spec written from
  now on; and `ui:"display"`, the field tag by which an entity says what its rows
  are called, read by `ui/resource` and refused at mount by `kit/rest`.

## Generated command controls

Generated resource commands follow the [resolved policy](resource/README.md):
the primary command is filled, destructive commands use danger tone, and ordinary
commands use outline buttons. New/Edit become secondary when a primary command
is present. Each command retains its named native POST form and submit button.

Each argument has a deterministic DOM ID scoped to its command action (including
the record path), locale and field name. In `resource.commandFormFor`, the format
is `pk-command-<hex(action)>-<hex(locale)>-field-<hex(field)>`; map entries append
`-key-<hex(key)>`. Each segment encodes the original UTF-8 bytes separately, so
punctuation, empty locales and feedback suffixes cannot alias another control.
Ordering and translated display words do not affect identity. The same command
instance rendered again retains its IDs; the generated screen offers it once.

**Reused** — `forms.ControlProps.ID`, the existing hex field-identity convention,
and component-owned label, help and error references. **Added** — command scope
for ordinary arguments, hidden locale controls and map entries. **Made reusable**
— every generated command inherits this scope without changing submitted names,
revision values or public control signatures. Lone controls retain their
name-derived fallback IDs. The [identity cases](resource/command_identity_test.go)
check complete documents and stable IDs; forms' existing instance tests check
scoped error references, since command forms do not currently render errors.

- **Reused** — the existing `components.Button` outline variant, `design.ResolveColors`,
  shipped palettes and resource rendering fixtures.
- **Added** — the outline choice for generated command submits and assertions for
  action prominence, label contrast, paragraph sizes and native submission.
- **Made reusable** — every application using `ui/screens` inherits the command
  styling through `ui/resource` without changing its resource declarations.

The assertions live in [resource](resource/generated_screen_floor_test.go),
[command controls](resource/command_controls_floor_test.go) and
[style](style/semantic_colors_test.go); run `go test ./ui/resource ./ui/style`.
The paragraph-size assertion covers the resource body; complete frame sizing and
product palettes still need browser measurements of the composed screen.

## Next action

To serve a page, compose the stylesheet once with `ui.Compose`, mount
`ui.Assets` beside the API and return views through `page.Serve`; the
[web module](../modules/web/README.md) is the smallest complete shell. To
render the same document or screen without the router — a preview, an export,
a test — call `document.Document` or `resource.List` with values. To add
a component, add its renderer and class lists in `ui/components` and its entry
to `examples.Gallery`; `go test ./ui/components/...` proves every emitted class
resolves to a rule. To change colours or type, supply a `design.Pair`.

## Served design floor

[The canonical test instrument](testdata/design_floor.mjs) measures rendered geometry,
typography, contrast, spacing, primary actions, paragraph measure, overflow and linked
stylesheets. [The reference-app test](../apps/platformkit/shell_design_floor_test.go)
runs it under `make check` on the dashboard, populated list, record, create and edit
forms at 390 and 1440px, in light/dark and English/Portuguese. Missing browser tools,
assets or measurements fail the test. Install the pinned browser prerequisites with
`npm --prefix e2e ci` and `npm --prefix e2e run install:browsers` before the Go gate.
[The e2e adapter](../e2e/design_floor.ts) retains its existing helper API and delegates
to the same instrument. The floor is a mechanical presentation check; keyboard and
screen-reader journeys remain separate verification.

The instrument is a port and the numbers are the loop's: `design_gate.py` owns
them, and [the boundary cases](testdata/design_floor_cases.mjs) — what
`go test ./ui` asserts — refuse a port that drifted from them. They cannot refuse a
gate that moved them: the gate sits outside this repository and no test here reads
it, so the two sets of numbers agree only while someone reads both. Three
routes run the gate itself instead of the port, behind four opt-in
doors, each of them opt-in because it reads files outside this repository.
`PLATFORMKIT_SHELL_DESIGN_GATE=<path to
design_gate.py>` makes that reference-app test run the gate by URL at
`--widths 390,1440` on the five pages it measures with the port.
`PLATFORMKIT_PICTURE_DESIGN_GATE=<path to design_gate.py>` makes
[the composed-picture test](../apps/platformkit/composed_picture_test.go) run the
same gate, at the same widths, through a proxy that carries the administrator's
session, on `/app/picturefixture/pictures` — the generated list page whose row opens
a composed picture door, which neither of the other two routes reaches.
`DESIGN_GATE_DIR=<directory holding the gate>` and `DESIGN_GATE_OUT=<directory for
its dumps>` make [the generated-page journey](../e2e/generated-pages-design-gate.spec.ts)
— `./scripts/e2e.sh generated-pages-design-gate` — judge six framed pages fetched
behind the administrator's session and written out with a `<base>` pointing their
sheet back at the live server, plus sign-in and the public home by URL; the journey
skips where either variable is absent, and a page written to disk is judged on its
markup and computed style alone, since a `file://` document cannot fetch the sheet
it links. `make check` therefore judges the port; the gate runs when a caller names
where its files are.

`go test ./ui` holds this paragraph to the tree: a door named here must still be read
out of the environment by a test or journey that starts a process in the same file, a
door naming where the gate lives must hand that process the value the environment gave
it rather than a path written out by hand in its place, and a door naming where the
gate's dumps go must be answered at every call that puts bytes on disk in the file
reading it, each writing at a path built from the value read. Each check reads one
file at a time, so a write carried into a helper of its own must carry the door's read with it.

**Reused** — the port, `shell_design_floor_test.go`'s rendered run of the reference
app's five pages, the loop's gate behind the doors named above, and the journey that
already judges eight pages by it. **Added** — the
naming of those variables where the floor is documented: no README, Makefile line or
CI file carried them, so the gate behind the port was runnable only by someone who
already knew they existed. **Made reusable** — the invocation, in the section a
reader of this floor already reaches.
