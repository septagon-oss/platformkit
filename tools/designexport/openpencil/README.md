# OpenPencil adapter tooling

This directory owns the native boundary of the existing
[design export](../README.md). The Go tokens, glyphs,
typed examples and stylesheet remain the source of truth. There is no second
component registry, page language or client-specific library here.

The generator packages tokens, icons and explicitly selected experimental native
components, using supplied-font validation, browser observations and a pinned
SDK correction layer. Pages and flows are not converted yet; this is not a
complete published component library or the finished shared provider interface.
With IBM Plex Sans 400/500/600, headless Chromium (`--font-render-hinting=none`),
light mode and a 1280×900 viewport, capture accepts all 107 gallery examples.
Native construction accepts 12: ten Button variants,
bare Input and Form; it explicitly refuses 95. These are measured guards, not proof of
complete typography, visual, interaction or provider support.

## Generate a design document

Install the dependencies below, then run from this directory for tokens and icons:

```sh
npm run generate -- /tmp/platformkit-foundation.fig
```

The command runs one fresh Go export from this checkout. Use an absolute `.fig`
path with an existing parent outside the workspace. Existing files and symlinks
are never overwritten; choose a new name for each export. Publication, deployment
and applying source proposals are separate operations.

To include components, install Chromium as described below and append repeated
`--example ID` selections and `--font FAMILY WEIGHT STYLE /absolute/font.woff`
arguments. Quote family names containing spaces; supply every required static face
with its actual family, weight and style. No brand font is selected implicitly.
For example, `--example pk-ui.component.form/default` selects the linked Form.
Optional `--mode light|dark` and `--viewport WIDTHxHEIGHT` choose one observation
profile; defaults are light and 1280×900. Every requested example must pass or no
file is created. Construction and source correspondence are checked across two
saves. Ordered layout is repeatable; native IDs and FIG bytes are not.

Open the FIG and inspect Foundation: 22 colors, three CSS font-family strings,
27 icon masters and 54 mode-bound light/dark previews. These strings are not
installed fonts or native text styles. Component definitions and Editable source
instances occupy separate pages; edit the latter's native properties. Each root
has one source correspondence, not duplicate theme or responsive claims. FIG
retains font identities and glyph outlines, not font files: editing requires the
same fonts separately in the editor. No page prototypes are included.

[buildComponentDocument](document.mjs) accepts one existing `ui.Export` snapshot,
explicit `examples`, `fonts`, `mode`, `viewport`, and caller-owned `browser` and
`renderer`. It returns foundation handles plus definition/placement frames and
exact selection handles. [buildFoundation](foundation.mjs) supplies the shared
`{ graph, collection, icons }`; `prepareIcon` validates glyphs without creating nodes.
These compose the existing source, not another catalog. Source hash, scope and
attribution remain on the Foundation frame and masters. In-process APIs trust
the caller's producer hash; origin records do not prove an edited file is fresh.

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
paint observations, text regions, native text controls and Chromium's font evidence. It
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

The Button and Input constructors mark their actual `label` text with paired
`<!--pk-text:label-->` comments. They preserve escaping and layout, including
empty Button labels beside icons. Input omits an empty label; its required
asterisk remains outside the editable region. Standalone Label marks `text`.
Button content-slot replacement and icon-only rendering omit the label region.
Capture records exact markers rather than guessing from visible
strings. Typed ownership, native property binding and named-slot replacement
remain the converter's responsibility; a balanced marker is not readiness proof.
Rendered Button slots also carry paired `pk-slot:IconStart`, `pk-slot:IconEnd`
or `pk-slot:Content` comments. Capture retains ordered slot groups, including
empty or nested content, without inventing layout boxes. Missing markers mean
that branch supplied no rendered slot, not that the source lacks a declaration.
Malformed or crossed boundaries are refused. The existing example declarations
remain authoritative; nested slot names alone do not establish component ownership.
Capture checks source child byte spans against their exact UTF-8 output, then
uses temporary numeric boundaries to identify single DOM roots. It removes those
boundaries and verifies that HTML parsing stayed unchanged before observing.
Each identified element carries its exact source path, component identity and
declared slot; names and sibling positions never establish correspondence.
Empty, multi-root and text-plus-element fragments receive no single-root claim.
Missing spans remain unobserved; crossed, ambiguous or parser-altering boundaries
are refused rather than repaired into guessed ownership.
Canonical text Inputs carry `data-pk-value="value"`. Their control observation
retains actual type, value, placeholder and exact control-element font evidence,
not a fabricated DOM text region. An empty control without a placeholder has no
observed glyphs; a visible placeholder can supply its own fonts. Native binding
must check the typed source value, including explicit omitted-string defaults,
and refuse unsupported placeholder behavior or browser-normalized mismatches.
Icons separately expose their requested name and source-resolved
`data-pk-icon-canonical` identity. Capture retains both, including aliases and
fallbacks; an adapter must still verify the canonical asset and its provenance.
SVG observations retain ordered children, exact attributes and computed geometry,
fill/stroke dependencies and presentation, so canonical markup cannot conceal
CSS changes to paths, paint or effects.

Paint observations probe the existing root color variables with two distinct
values. This distinguishes tested direct aliases and mixed paints from unrelated,
equal-colored literals. `directCandidate` names an observed alias candidate,
not a binding guarantee. These are observed dependencies, not a general CSS
expression parser or proof for arbitrary functions and locally overridden themes.
They must not be used to advertise universal token-binding support.

## Construct an experimental native component

[materializeComponent](components.mjs) takes an existing native graph and
definition-parent ID, its source snapshot and observation, exact supplied font
faces, a live Skia renderer, and an explicit native color-collection ID. The
optional final argument supplies `{ region, master }` pairs for exact observed
slot regions and native icon masters. The parent's effective native mode must
match the observation. The result contains a master, its property definitions and
`components` entries with exact source paths, masters and property handles;
create linked instances through the existing graph API. This is an OpenPencil
adapter API, not the unfinished shared provider interface or a library publisher.

The text-row capability supports one explicitly bound, nonempty text region in a
centered, unconstrained, nonwrapping horizontal flex container, optionally with
named slots containing one canonical SVG each. Construction
preserves observed solid paints, corner radii and padding including transparent
border insets. Observed direct token aliases become native bindings only when
the selected collection's identities and values match every source theme.
Literal paints remain unbound; ambiguous, stale and derived paints are refused.
This uses capture's measured alias evidence, not a general CSS equivalence proof.
Construction requires successful native text measurement and refuses outer
geometry differences larger than 1/64 CSS pixel. Rejection removes the newly
constructed nodes and restores the caller's measurement hook.

Nested construction adds observed vertical stretch/fill and end-aligned rows,
single-line label fragments, text controls and uniform solid borders. It builds
separate linked masters for each source invocation, preserving different baselines
even when Cancel and Create share the Button interface. The real Form is the
vertical proof: Input and actions, with Cancel and Create inside actions. This
does not model submission or make the native drawing an interactive web form.
Text values use an unwrapped, auto-width native text node inside a fixed clipping
viewport. The existing auto-size operation supplies measured bounds, including
genuine zero advance and clearing; failed measurement rolls back the edit and
history. Empty controls provide no glyph evidence. Caret, selection scrolling and
other interactive input behavior are outside this static conversion.
The Form checks cover both themes at 320px and 1280px, then nested Input and
Button edits, undo/redo, sibling isolation, two saves and Go proposal reprojection.
Run the focused local proof from this directory after the prerequisites above:

```sh
node --import ./register.mjs --test --test-name-pattern='Form|Input|composition' browser/components.test.mjs
```

[bindComponentProperties](bindings.mjs) preflights exact constructed handles before
binding source strings to native `TEXT` properties and the supported single-icon
occurrences to `INSTANCE_SWAP`. This restricted projection does not redefine Go
content slots as single replacements. Native definitions and references drive behavior.
Provenance records the source invocation, font identities, viewport, observed
environment and a versioned native-property-ID to source-field map. Native display
names can change without changing that map; provenance is not authentication.
Explicit slot-property maps distinguish icon assets from nested source components;
asset instances do not acquire string-proposal ownership by being inside a slot.

[associateSourceInstance](source-changes.mjs) explicitly associates the exact
instance returned by `graph.createInstance` with one root source occurrence.
Preview siblings stay unmapped; copied correspondence is refused as ambiguous.
Reusable child templates carry only relative local IDs and declared slots, not
absolute placement paths. Association validates the complete mapped subtree;
`extractSourceProps` accepts an exact root or nested instance and derives its
path through native lineage, including after FIG import. It checks the canonical
snapshot and returns a provider-neutral `{baseSHA256, path, props}` proposal.
Only mapped, unconstrained string properties are supported. Native definitions,
lineage, baseline values and assignments must agree with the source and rendered
text. Extraction does not mutate the graph or snapshot. `no-supported-changes`
means only that the bound strings are unchanged, not that the document is equal.
Missing, malformed, stale and unsupported correspondence have explicit results.
Validate proposals through [ui.ProjectProps](../../../ui/proposal.go), or Core's
`go run ./tools/designexport --proposal` with proposal JSON on stdin. It reuses Go
constructors and returns a candidate snapshot without editing files. String
extraction is not semantic slot replacement, scene equivalence
or a source persistence service. The foundation generator remains unchanged.

Browser tests compare the actual Go Button, including 16px leading and 20px trailing
icons, in both themes with supplied IBM Plex Sans 600 and explicit
`--font-render-hinting=none`. Label edits, icon swaps, undo/redo and two FIG round
trips retain native links and match source dimensions and icon placement within
1/64 pixel. Fresh native derived-layout records preserve sibling positions after
text grows; edits after reopening must still reflow. The precision correction preserves CanvasKit's
shaped advances; it does not reproduce every platform's font hinting. These are
geometry and persistence checks, not an accessibility audit or a complete source
pixel comparison. Separate assertions change palette values and verify native
painted pixels after each save. Page-mode serialization and imported instance
index corrections keep the native mode and live links from silently disappearing.

Icon composition verifies canonical flat path/circle geometry, attributes and
observed paints; grouped, transformed or potentially clipped SVGs are refused.
Arbitrary nested content, nonuniform or nonsolid borders, outlines, filters,
wrapping and unimplemented sizing constraints remain unsupported. Text controls
support empty values; empty or collapsed-whitespace flex labels need separate
participation semantics. Native text properties still accept them, so preserving
their identity alone does not prove the resulting source layout. Do not
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
nested masters or editor drag handles. The source-conversion checks above cover
only their stated icon-slot subset, not generic nested component replacement.

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

The locked IBM Plex Sans fixtures cover 400/500/600 Latin faces: exact bytes,
native shaping, Chromium face selection and pixels across two saves.
The OpenType correction retains Node outlines. The browser's local-font fallback
checks binary family/weight/style metadata when legacy display names fail SDK
lookup; unreadable, mismatched or ambiguous candidates do not select a face.

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

The [editor check](editor/replacement.test.mjs) refuses stale builds and tests icon
swaps and generated Form/Button properties through history and two downloaded saves.
Fresh contexts verify links, proposals and unchanged masters, siblings and placement positions.
Font settings grants access to process-only OTF fixtures; file digests prove the
loaded bytes. CI explicitly treats its isolated HTTP origin as secure. Saving loads
unopened pages without replaying edits. Tree navigation no longer creates canvas nudges; native OS
pickers, hardware GPUs and a full accessibility audit remain unverified.
No document, WebGPU assets or product fonts are packaged; PWA registration is disabled.

Native properties and FIG override paths retain links through reflow and undo;
repeated saves merge overrides by path. Low-level property fixtures use a
deterministic measurer, while component comparisons use the supplied real fonts.
Normal graph subscriptions and history are checked after deferred notifications
settle. Property operations pause synchronization only while computing/restoring
captured geometry; authored parent resizing still propagates master dimensions.
Failed measurement restores grid sizing modes and frees the built Yoga trees.

Text-property guarantees cover placed root or nested targets and exact layout undo/redo.
Master-owned edits, variants and arbitrary imports remain unverified; identity
must be unambiguous. These tests are not a universal replacement contract.
The reordered-instance tests prove that labels retain the correct identity;
they do not prove persistence of per-instance child order. The SDK currently
rehydrates children in master order.

## Release blockers

The pinned app's full workspace audit still fails. Adapter pins replace the observed
affected browser imports; build tools, copied assets and complete notices still need review.
Font deployment, SVG fidelity, responsive/accessibility and source conversion remain
incomplete. Product prototypes need runtime-state mappings
and governed end-to-end persistence tests in their owning product repository.
Native sizing still needs evidence beyond the declared comparison cases, including
wrapping and whitespace-only flex participation during edits. Correct font loading
alone does not resolve those layout differences.
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
or editor release. Active CI enforces this gate after the locked install and
before the native tests. The adapter's npm-locked tree currently passes with zero reported
vulnerabilities; this is not a general security certification. Do not use
`npm audit fix --force` to downgrade the SDK or bypass its source checks.
The import-boundary test verifies that narrower native editing/FIG imports do
not load the expression or presentation packages. Calculator and presentation
tests intentionally exercise broader entry points. Reachable browser-surface
verification and the other release requirements above remain unfinished.

This tooling neither deploys the editor nor installs dependencies in the Go image.
OpenPencil attribution and license terms are recorded in the root
[NOTICE](../../../NOTICE).
