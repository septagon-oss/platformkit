# OpenPencil adapter tooling

This directory owns the native boundary of the existing
[design export](../../../README.md#build-a-screen). The Go tokens, glyphs,
typed examples and stylesheet remain the source of truth. There is no second
component registry, page language or client-specific library here.

The implemented boundary is a tokens-and-icons FIG generator, supplied-font
validation, browser observations, experimental single-text component construction
and a pinned SDK correction layer with conformance tests. The generator does not
include these experimental components; pages and flows are not converted yet.

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
`prepareIcon` validates SVG into caller-owned native properties and paint fields
without creating nodes; the generator consumes that same preparation.

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
interaction or comparison with an independent SVG renderer. Separate supplied-font
tests exercise actual shaping within the boundary described below.
This suite supplements `make check`'s Go and repository-policy gates.

`npm run test:stock` deliberately omits the corrections. It reproduces the
upstream native failures and is expected to exit nonzero; it is not a release
gate that should be made green by removing assertions.

## Observe component inputs

[captureExample](browser/capture.mjs) accepts a caller's Playwright Chromium
browser, the existing Go snapshot, one exact example ID and optional mode,
viewport and supplied fonts. It returns source identity, computed layout and
paint observations, text regions and the faces Chromium used for each region. It
does not construct native components or assert that those observations can all
be represented faithfully in a FIG file.

Launch Chromium with `args: ['--enable-automation']` so capture can inspect its
rendering environment. Observations retain the browser product, protocol version,
headless flag and explicitly selected font-hinting mode, never the full launch
arguments or user-agent string. `fontHinting: 'default'` means no explicit flag;
it does not promise the same default across browsers or operating systems.

Each observation uses a disposable, unauthenticated document. Resources must be
supplied in memory; executable content and application controllers do not run.
Capture flushes descendant styles and allows one second for finite motion to
settle; paused or perpetual animations are refused. Tests compare both themes
and two viewports with independent source HTML, not full responsive or
accessibility coverage. Install Chromium, then run the local browser checks:

```sh
npx playwright install --with-deps chromium
npm run test:browser
```

The first command downloads a browser and may install system dependencies.
CI runs these checks alongside the native suite and `make e2e`.

The Button constructor marks its actual `label` text with paired
`<!--pk-text:label-->` comments. They preserve escaping and layout, including
empty labels beside icons. Content-slot replacement and icon-only rendering
omit the region. Capture records exact markers rather than guessing from visible
strings. Typed ownership, native property binding and named-slot replacement
remain the converter's responsibility; a balanced marker is not readiness proof.
Rendered Button slots also carry paired `pk-slot:IconStart`, `pk-slot:IconEnd`
or `pk-slot:Content` comments. Capture retains ordered slot groups, including
empty or nested content, without inventing layout boxes. Missing markers mean
that branch supplied no rendered slot, not that the source lacks a declaration.
Malformed or crossed boundaries are refused. The existing example declarations
remain authoritative; nested slot names alone do not establish component ownership.
Icons separately expose their requested name and source-resolved
`data-pk-icon-canonical` identity. Capture retains both, including aliases and
fallbacks; an adapter must still verify the canonical asset and its provenance.

Paint observations probe the existing root color variables with two distinct
values. This distinguishes tested direct aliases and mixed paints from unrelated,
equal-colored literals. `directCandidate` names an observed alias candidate,
not a binding guarantee. These are observed dependencies, not a general CSS
expression parser or proof for arbitrary functions and locally overridden themes.
They must not be used to advertise universal token-binding support.

## Construct the experimental text component

[materializeComponent](components.mjs) takes an existing native graph and
definition-parent ID, its source snapshot and observation, exact supplied font
faces, a live Skia renderer, and an explicit native color-collection ID. The
parent's effective native mode must match the observation. It returns a master and its
property definitions. Create linked instances through the existing graph API;
this helper does not publish a library or add another component registry.

The current supported input is one explicitly bound, nonempty text region in a
centered, unconstrained, nonwrapping horizontal flex container. Construction
preserves observed solid paints, corner radii and padding including transparent
border insets. Observed direct token aliases become native bindings only when
the selected collection's identities and values match every source theme.
Literal paints remain unbound; ambiguous, stale and derived paints are refused.
This uses capture's measured alias evidence, not a general CSS equivalence proof.
Construction requires successful native text measurement and refuses outer
geometry differences larger than 1/64 CSS pixel. Rejection removes the newly
constructed nodes and restores the caller's measurement hook.

[bindTextProperties](bindings.mjs) binds exact constructed text handles to
unconstrained source string properties, after preflighting every target. It uses
native definitions and references, not names or plugin metadata to drive behavior.
Provenance records the source invocation, font identities, viewport and observed
environment; it is not an authentication or edited-document freshness check.

Browser tests compare the actual Go Button in both themes with supplied IBM Plex
Sans 600 and explicit `--font-render-hinting=none`. Label edits, undo/redo and two
FIG round trips retain native links and match independently rendered source
dimensions within 1/64 pixel. The precision correction preserves CanvasKit's
shaped advances; it does not reproduce every platform's font hinting. These are
geometry and persistence checks, not an accessibility audit or a complete source
pixel comparison. Separate assertions change palette values and verify native
painted pixels after each save. Page-mode serialization and imported instance
index corrections keep the native mode and live links from silently disappearing.

Icons inside source component conversion, slots, visible
borders, wrapping, constrained widths and empty or collapsed-whitespace inputs
remain unsupported. Native text properties still accept empty values; preserving
their identity does not prove whitespace-only flex layout after an edit. Do not
treat this development boundary as a complete editable component library.

Native creation, updates and replacement retain FIG's authored `uniformScaleFactor`.
Flat, solid-painted vector masters scale from FIG-representable canonical geometry,
including strokes, without detaching or cumulative resizing. Tests cover 20px and
16px instances, composition, master updates, factor edits and clearing.
Direct swaps retain geometry and links; placed-instance `INSTANCE_SWAP` tests cover
repeated slots, undo/redo and two saves without changing siblings or masters.
File-level references and strict rendered pixels are checked independently of the
importer. Replacing locally edited descendants is refused before mutation because
subtree history is unfinished. Stroke caps and joins survive serialization;
conflicting styles, unsupported scaled masters and nonrepresentable geometry are refused.
For flat vector replacements, consistent source-occurrence color-variable mappings
also survive history and two saves. Correspondence uses native variable identities,
not token names or vector positions; ambiguous, partial or missing roles are refused.
Literal-only vectors retain their existing inheritance. Swapped bindings become
explicit native overrides: token value changes remain live, but later source-role
reassignment does not automatically retarget those overrides.
This does not establish scaling for text, effects, dashed vectors, arbitrary
nested masters, editor drag handles or source-owned icon-slot replacement.

[Synchronization](sync-correction.mjs) plans and validates a projected result before
applying changes through native graph APIs. Planning allocates no native IDs and
emits no graph events; SDK copy helpers are reused. Geometry calculation and source
identity reconciliation contain no product or catalog rules. The effect boundary
owns ID allocation, notifications and temporary deletion ancestry, so a missing
reference is never treated as evidence of a deletion.
Nested instances retain their own root and descendant overrides during a
containing-component synchronization; explicit outer paths retain precedence.

Native fill/stroke bindings and literal or empty paints retain their tested
values through two saves and subsequent synchronization. Binding-only stroke
edits still inherit master stroke-width changes. These checks do not establish
inheritance of every future master paint attribute, including opacity or count.

Tests cover path additions, removals and reordering, empty-master recovery,
unscaled geometry, nested identities, explicit geometry and paint overrides, and
two saves. Normal editor deletion notifications are checked without manual sync.
Unrelated invalid components remain untouched. Unlinked local children survive;
linked children without unambiguous source correspondence are refused.

## Supply exact font faces

[fonts.mjs](fonts.mjs) accepts caller-owned static TTF, OTF or WOFF files as
`{ family, weight, style, bytes, sha256 }`. `validateFonts` verifies copied bytes
against their digest and internal face metadata without registering a font.
`loadFonts` additionally accepts required `{ family, weight, style, text }` values,
checks exact faces and glyph coverage with OpenType and CanvasKit, then registers
the supplied bytes in the existing SDK font manager. It does not resolve CSS
fallback stacks or download fonts. WOFF2 and variable fonts are refused until
both the shaping and FIG-outline paths support them.

The locked IBM Plex Sans test dependency supplies real 400 and 600 Latin faces.
Tests verify their byte identities, native shaping, Chromium's actual custom
face selection, and unchanged native pixels through two saves with those same
fonts loaded. The source-checked OpenType import correction prevents the SDK
from silently omitting derived glyph outlines in Node.

A FIG contains editable font references and derived outlines, not the font
files themselves. The SDK's font-free outline fallback is not shaping-equivalent
for kerning and ligatures. The live application and editor still need the same
licensed font files installed or hosted; these fixtures do not establish that
deployment. A process cannot replace an already loaded family/style with
different bytes because the SDK caches FIG font digests by that identity.
Use a fresh process for a different font revision. Fixture provenance and the
complete OFL notice are recorded in [NOTICE](../../../NOTICE).

## Understand the correction boundary

[register.mjs](register.mjs) checks the SDK versions and installs a process-local
Node loader. [corrections.mjs](corrections.mjs) rejects changed upstream bytes.
[Dockerfile](Dockerfile) applies the same guards to main and worker browser builds
through [build-editor.mjs](build-editor.mjs), using pinned source, images and locks.
Build locally from the repository root:
`docker build -f tools/designexport/openpencil/Dockerfile -t platformkit-openpencil:local .`.
Serve that disposable image on a free loopback port (here 18089), container port 8080,
UID/GID 101 and writable `/tmp`, `/var/run` and `/var/cache/nginx`. Its `/healthz`,
`/platformkit-provenance.json` and `/licenses/bundled-dependencies.json` expose
health, build-input hashes and bundled dependency notices. CI builds and tests
the editor without publishing. From this directory, set the test image's URL:

```sh
PLATFORMKIT_OPENPENCIL_URL=http://127.0.0.1:18089 node --import ./register.mjs --test editor/*.test.mjs
```

The [editor check](editor/replacement.test.mjs) refuses stale build inputs before
opening disposable documents. It uses the real property picker, keyboard history,
parse/export workers and two downloaded FIG saves in fresh browser contexts.
Source-owned icon color bindings, master/sibling geometry, fallback paints, links
and occurrence references are checked.
Chromium uses software WebGL and its file-input/download fallback; native OS
pickers, hardware GPUs and a full accessibility audit remain unverified.
No document, WebGPU assets or product fonts are packaged; PWA registration is disabled.

Native properties and FIG override paths retain links through reflow and undo;
repeated saves merge overrides by path. Low-level property fixtures use a
deterministic measurer, while component comparisons use the supplied real fonts.
Normal graph subscriptions and history are checked after deferred notifications
settle. Property operations pause synchronization only while computing/restoring
captured geometry; authored parent resizing still propagates master dimensions.
Failed measurement restores grid sizing modes and frees the built Yoga trees.

Text-property guarantees cover single placed targets and exact layout undo/redo.
Master-owned edits, variants and arbitrary imports remain unverified; identity
must be unambiguous. These tests are not a universal replacement contract.
The reordered-instance tests prove that labels retain the correct identity;
they do not prove persistence of per-instance child order. The SDK currently
rehydrates children in master order.

## Release blockers

The pinned app's full workspace audit still fails. Adapter pins replace the observed
affected browser imports; build tools, copied assets and complete notices still need review.
Supplied fonts, SVG fidelity, responsive/accessibility, interactive editing, typed conversion
and source freshness remain incomplete. Product prototypes need runtime-state mappings
and governed end-to-end persistence tests in their owning product repository.
Native component sizing still needs evidence beyond the declared single-text
comparison environment, including visible borders, wrapping and whitespace-only
flex participation during edits. Correct font loading alone does not resolve
those layout differences.
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

The SDK's presentation dependency is separately aliased to
[`@neo-ma/pptxgenjs@4.3.0`](https://github.com/NeomaVerwaltung/PptxGenJS/releases/tag/v4.3.0),
whose published package removes the vulnerable `image-size` dependency. The
OpenPencil versions and source-hash checks remain unchanged. The loader verifies
both replacement package identities without loading their executable modules.
Presentation tests call the actual public SDK export through `@open-pencil/core/io`
and inspect the resulting OOXML: editable text and shapes, styling, geometry,
PNG relationships and bytes, unchanged source nodes, and explicit failure states.
Both published ESM and CommonJS builds are exercised. The supplied PNG callback
tests the image boundary, not browser rasterization or PowerPoint visual fidelity.

Run `npm audit --omit=dev --audit-level=high` before considering a native-tooling
or editor release. CI and the tagged-tree release workflow enforce this gate
after the locked install and before the native tests. The adapter's npm-locked tree currently passes with zero reported
vulnerabilities; this is not a general security certification. Do not use
`npm audit fix --force` to downgrade the SDK or bypass its source checks.
The import-boundary test verifies that narrower native editing/FIG imports do
not load the expression or presentation packages. Calculator and presentation
tests intentionally exercise broader entry points. Reachable browser-surface
verification and the other release requirements above remain unfinished.

This tooling neither deploys the editor nor installs dependencies in the Go image.
OpenPencil attribution and license terms are recorded in the root
[NOTICE](../../../NOTICE).
