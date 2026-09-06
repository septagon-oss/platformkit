# OpenPencil adapter tooling

This directory owns the native boundary of the existing
[design export](../../../README.md#build-a-screen). The Go tokens, glyphs,
typed examples and stylesheet remain the source of truth. There is no second
component registry, page language or client-specific library here.

The implemented boundary is a tokens-and-icons FIG generator, a pinned SDK
correction layer and native conformance tests. Component examples, pages and
flows are not converted yet. This tooling neither builds nor deploys the editor;
a passing native test is not a production release.

## Generate the foundation

After installing the dependencies below, run from this directory:

```sh
npm run generate -- /tmp/platformkit-foundation.fig
```

The command runs the existing Go export from this checkout, creates native
variables and linked icon components, and checks that the resulting FIG reopens
before creating the output. Use an absolute `.fig` path whose parent already
exists outside the workspace. Existing files and symlinks are never overwritten;
choose a new name for another export. Publication and deployment are separate
operations. The output is a generated snapshot, not a place to make source edits.

Open the file in OpenPencil and inspect the Foundation variable collection and
the Icon masters, light and dark frames. Core currently supplies 22 color
variables, three font-family strings, 27 icon masters and 54 linked preview
instances. Each preview inherits its frame's native variable mode. Font-family
strings preserve the CSS fallback stacks; they are not installed fonts, a
typography scale or native text styles. There are no page prototypes in this file.

[foundation.mjs](foundation.mjs) also accepts a snapshot from a product's existing
`ui.Export` call. It does not register another palette or icon catalog. Token
names and canonical icon names retain source identity; native file-local GUIDs
may change on import. Source hash, scope and attribution travel on the Foundation
frame, and each master carries its glyph provenance. These records describe
origin, not native component behavior or proof that an edited document is fresh.
The CLI obtains fresh source itself; the in-process API trusts its caller's
producer hash rather than attempting a second implementation of Go JSON hashing.

Supported inputs are the existing light/dark token contract, literal hexadecimal
colors, font-family strings and the canonical path/circle SVG glyphs with group
transforms and supported solid paints. Unsupported tokens, elements or attributes
fail explicitly. Nested SVG viewports, invisible shapes and transformed strokes
are rejected until their native fidelity is implemented and tested. Separate SVG
paths remain separate native vectors so mode
bindings also control multi-path icons. This is a fidelity check for trusted Go
exports, not an arbitrary SVG-upload sanitizer or a universal Figma converter.

## Run the native checks

Use Node 24 or newer. From this directory, install the locked dependencies and
run the tests:

```sh
npm ci --ignore-scripts
npm test
```

The tests create disposable scene graphs and FIG buffers in memory; CLI cases
write only to automatically removed temporary directories outside the workspace.
They do not open your documents or connect to an editor. Foundation tests compare
native variable values, master links, provenance and rendered icon pixels across
two successive FIG round trips. CanvasKit renders every light/dark icon in memory
without requiring a GPU. This verifies native raster persistence, not browser
interaction, installed-font shaping or comparison with an independent SVG renderer.
CI and the tagged-tree workflow run the same command. `make check` remains the
Go and repository-policy gate; this suite is an additional native boundary.

`npm run test:stock` deliberately omits the corrections. It reproduces the
upstream native failures and is expected to exit nonzero; it is not a release
gate that should be made green by removing assertions.

## Understand the correction boundary

[register.mjs](register.mjs) checks the SDK versions and installs a process-local
Node loader. [corrections.mjs](corrections.mjs) checks the SHA-256 of each affected
upstream module before transforming it. A changed or already-corrected source
is an error. Installed dependencies remain untouched. The same source transform
can be used at a browser build boundary, but that integration is unfinished.

The correction uses native component/property identities and FIG override
paths, not plugin metadata. Repeated saves merge overrides by path. Reflow and
undo preserve the linked component relationship; they do not detach instances.
The conformance fixtures use a deterministic text measurer, so their exact
dimensions demonstrate layout state rather than real font shaping.
Editor fixtures use the normal graph subscriptions and public history actions.
Assertions cover the state after deferred notifications settle, not just the
immediate return from undo. Genuine authored master changes still propagate;
the property operation pauses component synchronization only while computing or
restoring its captured geometry. Ordinary authored parent resizing still
propagates new master dimensions to instances. Failed text measurement restores
grid sizing modes and explicitly frees the successfully built Yoga trees.

Supported behavior is deliberately narrower than the editor's entire API:
single-target, placed-instance text properties, their nested references, native
save/reopen, and exact undo/redo of the affected layout state. Identity
correspondence must be unambiguous. Editing inside a component master, instance
swaps, variant replacement and arbitrary imported documents require further conformance
before they can be advertised as supported. Unsupported cases must not be
silently treated as passing replacement contracts.
The reordered-instance tests prove that labels retain the correct identity;
they do not prove persistence of per-instance child order. The SDK currently
rehydrates children in master order.

## Release blockers

The browser build still needs these corrections, actual font tests, independent
SVG visual-fidelity comparison, responsive and accessibility checks, and save/reopen
in the interactive editor. Typed component conversion and edited-document
source-freshness verification are unfinished. Product prototypes need runtime-state mappings and governed
end-to-end persistence tests in their owning product repository.
Programmatic batches with an unrelated master update already queued before a
property edit need further lifecycle work: wait for that preceding update to
settle before using the verified edit/history boundary. The current correction
does not make overlapping authored-change batches transactional.
Likewise, property-driven layout that resizes a neighboring master requires
downstream instance synchronization and history that are not implemented yet.
The edit is rolled back with an explicit error; it is not counted as supported
composition. This temporary guard prevents stale external instances while that
cross-component behavior remains a release requirement.

The SDK's expression dependency is overridden with `expr-eval-fork@3.0.3`,
the maintained fork addressing the published
[prototype-access](https://github.com/advisories/GHSA-8gw3-rxh4-v6jx) and
[unrestricted-function](https://github.com/advisories/GHSA-jc85-fpwf-qm7x)
advisories. The locked tarball contains the updated builds;
[the fork's changelog](https://github.com/jorenbroekema/expr-eval/blob/8212543faa2686054a25f49fe96b614aa5d9ea4c/CHANGELOG.md)
records the stale-build correction in 3.0.2. Source-checked corrections initialize the function
counter and give each SDK calculation its own parser, including each batch item.
This preserves multiple expression-defined functions without leaking them into
later calculations. Calculator tests exercise arithmetic and errors through the
real SDK tool, plus harmless callback/prototype rejection in both module formats.
These checks are not a general-purpose sandbox or a browser-security approval.

Run `npm audit --omit=dev --audit-level=high` before considering a native-tooling
or editor release. It still fails with three high-severity affected packages:
`image-size`, its dependent `pptxgenjs`, and the SDK that depends on it. This is
an unresolved security gate, not an accepted exception. Do not run
`npm audit fix --force`: its proposed SDK downgrade invalidates the pinned
corrections. Dependency remediation and reachable browser-surface verification
remain required. The import-boundary test verifies that the narrower native
editing/FIG imports do not load the expression or presentation packages; the
calculator tests intentionally use the broader tools entry point. Neither
observation waives the remaining audit failure.

Nothing in this directory changes the deployed editor or authorizes release.
The reference Go application's container does not install these dependencies.
OpenPencil attribution and license terms are recorded in the root
[NOTICE](../../../NOTICE).
