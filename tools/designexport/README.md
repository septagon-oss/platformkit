# Design export tooling

This tool projects PlatformKit's existing Go components for adapter development.
The tokens, glyphs, typed examples and stylesheet remain the source of truth;
the export is neither a second component catalog nor a JSON page runtime.
For the shared contracts and trust boundaries, read
[Architecture](../../ARCHITECTURE.md#compose-the-interface).

## Inspect the source

Use the Go version in [go.mod](../../go.mod). Run these commands from the
repository root; they need no running application or database:

```sh
go run ./tools/designexport
go run ./tools/designexport --example pk-ui.component.button/primary
```

The first command writes the complete gallery snapshot to stdout. The second
selects one existing example by its stable ID. Both include typed properties and
schemas, named slots, rendered HTML, CSS, theme tokens and attributed SVG glyphs.
Captured children retain local identities, declared ownership and same-render
byte spans; opaque inputs and unobserved children remain explicit. Font-family
values are fallback stacks, not bundled fonts. A hash addresses the snapshot.

Products call [ui.Export](../../ui/export.go) with their own palette, bound
examples and stylesheet additions. Core's IDs come from
[Gallery](../../ui/components/gallery.go); labels and grouping are not identities.

## Inspect typed foundation values

Opt into the source-token contract with an explicit mode selection:

```sh
go run ./tools/designexport --tokens both
```

Use `light` or `dark` to select one mode. This writes a v2 foundation snapshot
with `source-tokens.v1`, typed colour references, ordered font families, scales,
shadows and timing declarations. It includes the normal CSS, icons and both
legacy theme records, but no component examples; the typed selection alone is
mode-scoped. Other theme token kinds, including semantic shape dimensions, are
not part of this typed projection. No fonts are bundled and stdin is not read.
The command cannot be combined with edit or example-selection flags.

For a standalone token/asset selection, products use
[ui.ExportTokens](../../ui/export_tokens.go) without capturing components, CSS or
icons. Validate any selected subset, then use `DesignExport.WithTokens` to attach
it to a product capture when needed. This opt-in leaves default v1 exports
unchanged. The [native adapter](openpencil/README.md#generate-a-design-document)
accepts an explicitly selected v2 colour/numeric subset, not this complete Core
selection. This command inspects the source contract; it does not produce DTCG
files or a complete editable library.

For DTCG interchange, call `TokenExport.DTCG(mode)` on an explicitly selected
package. The [runnable palette/family example](../../ui/export_dtcg_test.go)
shows that composition; verify it locally with
`go test ./ui -run '^ExampleTokenExport_DTCG$' -v`, which should pass without
services or filesystem output. The method returns JSON bytes, diagnostics and
an error. An unsupported value means no document; successful output also embeds
any declaration-loss diagnostics and source metadata. Review those diagnostics
before writing the returned bytes to a `.tokens.json` file. A full Core selection
contains unsupported contextual units and transitions, so it is refused rather
than silently reduced. The [architecture contract](../../ARCHITECTURE.md#compose-the-interface)
defines the subset; native editing and source write-back are separate gates.

## Project a property edit

Add `--props` to read a typed property patch from stdin:

```sh
printf '%s\n' '{"label":"Create album","loading":true}' |
  go run ./tools/designexport --example pk-ui.component.button/primary --props
```

The output contains the selected, changed invocation and the complete stylesheet,
themes and icons. [Example.WithProps](../../ui/components/example.go) enforces
exact public field names and types. Duplicate keys, unknown or internal fields,
trailing JSON and edits to read-only helpers are refused. JSON input is limited
to 1 MiB. Plain exports and selections do not read stdin.

## Validate an editor proposal

A proposal addresses the full current export hash and exact root and local child
ID segments. This example requires `jq`:

```sh
go run ./tools/designexport |
  jq '{baseSHA256: .sha256, path: ["pk-ui.component.form/default", "actions", "create"], props: {label: "Create"}}' |
  go run ./tools/designexport --proposal
```

To replace that invocation, use `--replacement` and replace the `jq` field
`props: {label: "Create"}` with
`replacementPath: ["pk-ui.component.button/primary"]`. Both source interfaces
must agree. Replacement copies the selected invocation's complete inputs and
renderer while retaining destination metadata; it does not merge overrides.

These operations validate freshness and observed source ownership and return
the full candidate snapshot. A selected-example export is not the full base.
Unknown or repeated request fields are refused. Products use
[ui.ProjectProps](../../ui/proposal.go) or
[ui.ProjectReplacement](../../ui/replacement.go) with their own source composition.

All commands above project in memory: they do not edit source files, save an
application resource, execute a user interaction or deploy anything. Rendering
runs trusted Go constructors; arbitrary callback effects cannot be rolled back.
Slots accept trusted Go nodes through `WithSlot`, not user-supplied JSON nodes.
Persistence and authentication belong to the caller; native editing fidelity
requires separate adapter verification.

[OpenPencil tooling](openpencil/README.md) owns native document generation,
supported inputs, fonts and editor checks. Component conversion is partial;
pages and flows remain unfinished. Follow that guide for the measured limits,
not an assumption that a source snapshot is a complete editable library.
