# Design export tooling

This tool projects PlatformKit's existing Go components for adapter development.
Tokens, glyphs, typed examples and stylesheets remain authoritative. Products
reuse [export.Export](../../ui/export/export.go) with their own composition; the
[architecture](../../ARCHITECTURE.md#compose-the-interface) defines its boundaries.

## Inspect the source

Use Go from [go.mod](../../go.mod), at the repo root; no services are needed:

```sh
go run ./tools/designexport
go run ./tools/designexport --example pk-ui.component.button/primary
```

These write the full gallery or one captured invocation, with typed properties,
schemas, slots, HTML, CSS, themes, attributed glyphs and a content hash. Children
retain local IDs, ownership and spans; opaque/unobserved inputs remain explicit.
Core's IDs come from [Gallery](../../ui/components/examples/gallery.go).

## Inspect typed foundation values

```sh
go run ./tools/designexport --tokens both
```

Select `light`, `dark` or `both` for v2 `source-tokens.v1`: typed colours, font
families, scales, shadows and timing, plus CSS, icons and legacy themes. Other
theme token kinds and component examples are excluded; fonts are not bundled.
This reads no stdin and cannot combine with edit or selection flags.

Products compose [export.ExportTokens](../../ui/export/export_tokens.go), validate subsets
and attach them through `DesignExport.WithTokens`; default v1 exports remain
unchanged. The [native adapter](openpencil/README.md#generate-a-design-document)
accepts an explicitly selected colour/numeric subset, not the complete selection.

`TokenExport.DTCG(mode)` projects JSON with loss diagnostics and source metadata
to review before saving. Unsupported values, including a full Core selection's
contextual units and transitions, yield no document. Check the
[example](../../ui/export/export_dtcg_test.go) without services or filesystem output:
`go test ./ui -run '^ExampleTokenExport_DTCG$' -v`.

## Project a property edit

```sh
printf '%s\n' '{"label":"Create album","loading":true}' |
  go run ./tools/designexport --example pk-ui.component.button/primary --props
```

The output contains the changed invocation, stylesheet, themes and icons.
[Example.WithProps](../../ui/components/examples/example.go) enforces exact public field
names and types. Duplicate, unknown or internal fields, trailing JSON and edits
to read-only helpers are refused. Input is limited to 1 MiB; plain exports and
selections do not read stdin. This command changes no source files.

## Validate an editor proposal

A proposal uses the full export hash and exact root/local child ID segments.
This example requires `jq`:

```sh
go run ./tools/designexport |
  jq '{baseSHA256: .sha256, path: ["pk-ui.component.form/default", "actions", "create"], props: {label: "Create"}}' |
  go run ./tools/designexport --proposal
```

For replacement, use `--replacement` with
`replacementPath: ["pk-ui.component.button/primary"]` instead of `props`.
It copies compatible inputs/renderer while retaining destination metadata.
Both return full candidates in memory; selected-example bases and unknown or
repeated fields are refused. Products use [export.ProjectProps](../../ui/export/proposal.go) or
[export.ProjectReplacement](../../ui/export/replacement.go) with their own composition.

## Persist a string property

[ui/source](../../ui/source/source.go) verifies an existing string literal at an
explicit typed capture call. It reuses `go/packages` and `dave/dst`, preserving
comments and import aliases. From a dependency-complete module, prepare a review:

```sh
pkit_source=ui/components/examples/gallery.go
pkit_line=$(rg -n 'ExampleWithSlots\(info\("pk-ui.component.button/primary"' "$pkit_source" | cut -d: -f1)
pkit_sha=$(sha256sum "$pkit_source" | cut -d' ' -f1)
go run ./tools/designexport |
  jq '{baseSHA256: .sha256, path: ["pk-ui.component.button/primary"], props: {label: "Create"}}' |
  go run ./tools/designexport --source "$pkit_source" --line "$pkit_line" --sha256 "$pkit_sha"
```

Review `change.source` and its file/export hashes. Repeat the pipeline with
`--apply` to reverify and replace the source file; `applied` reports the result.
Stale files, build inputs or export hashes require a fresh review. Computed,
missing, promoted and nonstring properties are refused. Add `--column` when
multiple capture calls start on one line. Formatting follows Go's formatter.

For another product, pass `--dir MODULE_ROOT --producer ./path/to/main`, then
`-- PRODUCER_ARGS` if needed. Its trusted producer must emit a full snapshot,
and accept those arguments followed by `--proposal` with JSON on stdin. Builds
disable cgo, workspaces and `GOFLAGS`; the five-minute CLI limit includes builds.
Temporary overlays and module copies stay outside the repository and are removed.
Producers must be deterministic and free of external effects; runtime files and
network state are not revision-checked. Applications need not import this tooling.

Apply uses Unix advisory locking and same-directory atomic rename. It preserves
file mode, but not ownership, ACLs or extended attributes. An unrelated editor
can race the final check; this is not filesystem compare-and-swap. Other platforms
can prepare reviews but cannot apply. Slots still accept trusted Go nodes;
insertion, replacement persistence, authentication and deployment remain separate.

## Keep a design fixture compiling

The fidelity checks under [openpencil](openpencil/README.md) embed a Go program in
each test: [sourceFixture](openpencil/browser/fixtures.test.mjs) writes it out,
builds it and observes what the executable prints. Those programs consume
`kit/httpx`, `ui/screens` and `ui/resource` from no Go package, so the compiler that
checks a kernel change never saw one — a renamed field broke CI's browser suite
while every Go gate passed.
[fixtures-compile.test.mjs](openpencil/fixtures-compile.test.mjs) builds every
embedded program the way its fixture does, with no browser: `make check-fixtures`,
or `npm test` in that directory, which is the step CI runs before its browser
steps. A fixture that contradicts the API it composes now fails a ten-second check
instead of a browser run.

[OpenPencil tooling](openpencil/README.md) owns native generation, fonts and
fidelity checks. Component conversion is partial; pages and flows are unfinished.
