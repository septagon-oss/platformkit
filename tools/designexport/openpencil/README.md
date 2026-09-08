# OpenPencil adapter tooling

This directory owns the native boundary of the existing
[design export](../README.md). The Go tokens, glyphs,
typed examples and stylesheet remain the source of truth. There is no second
component registry, page language or client-specific library here.

The generator packages tokens, icons and explicitly selected experimental native
components, using supplied-font validation, browser observations and a pinned
SDK correction layer. Product pages and flows are not converted yet; this is not a
complete published component library or the finished shared provider interface.
With IBM Plex Sans 400/500/600/700, headless Chromium (`--font-render-hinting=none`),
light mode and a 1280×900 viewport, capture accepts 107 of 109 gallery examples;
the two Video examples refuse because media capture is unsupported.
Native construction accepts 19: eleven Buttons, three Inputs, Form, Grid, two Text
examples and invalid Textarea; the coverage test reports 88 refusals. These are measured guards, not proof of
complete typography, visual, interaction or provider support.

Consumers can configure `design.Theme.Typography` before `ui.Export`; empty roles
retain the default stacks. In a separate profile with display set to IBM Plex Sans
and its supplied 600 face, H1–H5 retain editable source text, wrapping, history,
semantics and two saves at 320px/1280px in both themes. H6's uppercase styling is
still unsupported. These checks do not select a brand font or install font assets.

## Generate a design document

Install the dependencies below, then run from this directory for tokens and icons:

```sh
npm run generate -- /tmp/platformkit-foundation.fig
```

By default, the command runs a fresh Go export from this checkout. To use another
producer, pipe one UTF-8 `ui.Export` JSON snapshot into `--snapshot-stdin` (32 MiB
maximum); Go is not invoked and invalid input never falls back to Core. Supplied
hashes do not verify freshness against current source. Use an absolute `.fig` path
with an existing parent outside the workspace; files and symlinks are never overwritten.
Publication, deployment and applying source proposals are separate operations.

To include components, install Chromium as described below and append repeated
`--example ID` selections and `--font FAMILY WEIGHT STYLE /absolute/font.woff`
arguments. Quote family names containing spaces; supply every required static face
with its actual family, weight and style. No brand font is selected implicitly.
For example, `--example pk-ui.component.form/default` selects the linked Form.
Add repeated `--variant ID PROPERTY /absolute/projection.json` for nonbaseline
`ui.ProjectProps` snapshots (32 MiB each); the ID must also be selected. These
caller-supplied projections are validated for correspondence, not source freshness.
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
`renderer`, plus optional `variants: [{ exampleId, property, snapshot }]`.
Family selections expose `family` as property owner, inherited `properties` and
all state masters in `components`; `master` and `instance` retain the baseline.
It returns foundation and definition/placement handles. [buildFoundation](foundation.mjs) supplies the shared
`{ graph, collection, icons }`; `prepareIcon` validates glyphs without creating nodes.

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
They do not open your documents or connect to an editor. Native checks supplement
`make check`, covering variable descriptions, links, provenance and two FIG saves.
CanvasKit verifies light/dark icon pixels without a GPU; supplied-font tests cover shaping.
Experimental [color variables](variable-color.mjs) reuse authored CSS and native input IDs.
Palette edits, modes and history retain formulas in versioned FIG plugin data alongside
resolved COLOR fallbacks. Changed external fallbacks, missing inputs and cycles refuse.
This is adapter-specific persistence, not formula support in other editors or palette-to-source writeback.

Native editor value edits (including undo/redo) and the automation API's value setter
validate a candidate variable map before writing. Variable and collection deletion
validate the remaining formulas before unbinding nodes; a collection's complete
internal dependency closure can be removed together. Cross-collection mode fallbacks,
refused edits, retained redo entries and two FIG saves have independent native tests.
The variables dialog keeps a refused operation's explanation until dismissed and
retains keyboard focus. Deletion buttons have names and remain visible without hover.
This does not certify successful deletion's undo restoration, mode-changing operations,
raw graph mutation or unrestricted formula creation; those remain separate safety work.

`npm run test:stock` deliberately omits the corrections. It reproduces the
upstream native failures and is expected to exit nonzero; it is not a release
gate that should be made green by removing assertions.

## Observe component inputs

[captureExample](browser/capture.mjs) accepts a caller's Playwright Chromium
browser, the existing Go snapshot, one exact example ID and optional mode,
viewport and supplied fonts. It returns source identity, computed layout and
paint observations, text regions, native text and choice controls, and Chromium's font evidence. It
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
Select declares its existing `value`, `values` and `options` fields through
`data-pk-value`, `data-pk-values` and `data-pk-options`. Capture preserves actual
option values separately from labels, selected options, disabled state and groups.
An observed browser default is not a source assignment. Closed selects retain their
browser display text and viewport; listboxes retain aggregate painted font evidence,
not per-option layout. These observations do not enable native Select construction:
typed choice edits, browser arrow paint and FIG persistence remain unverified.
Icons separately expose their requested name and source-resolved
`data-pk-icon-canonical` identity. Capture retains both, including aliases and
fallbacks; an adapter must still verify the canonical asset and its provenance.
SVG observations retain ordered children, exact attributes and computed geometry,
fill/stroke dependencies and presentation, so canonical markup cannot conceal
CSS changes to paths, paint or effects.

Paint probes vary opaque, partial and zero alpha; `directCandidate` is not a binding guarantee.
A unique unconditional `:root` definition may yield an `expressionCandidate` with
authored CSS and referenced custom properties. The [color evaluator](color-expression.mjs) checks source values
and every observed token probe; ambiguous, shadowed or mismatched candidates stay absent.
Inactive media and theme overrides also disqualify a definition or referenced alias.
These are scoped observations, not a general CSS parser or proof for arbitrary functions.

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
named slots containing one canonical SVG each. Rows and composed frames share
one planner for solid fills, uniform solid borders, radii and padding, retaining
transparent token-bound strokes and border insets. Direct aliases require matching palettes in every theme
and retain source RGBA through legacy CSS alpha rounding. This is candidate evidence,
not general CSS equivalence. Literal paints stay unbound. Unambiguous authored expressions
become COLOR variables in the existing foundation collection, keyed by their source CSS
custom-property names and bound to native input IDs. Matching roles are reused; conflicting
or edited roles refuse without being overwritten. Allocation follows successful construction
and geometry checks, and failed construction removes its own nodes and variables.
Supported text, fills, uniform borders and canonical SVG currentColor occurrences retain
these formulas through palette edits, history and two saves. Glyph swaps preserve occurrence
colour roles without changing canonical assets. Ambiguous, stale and unsupported expressions
remain refusals; this does not add layout capabilities or certify arbitrary CSS.
Construction measures text and rejects
geometry differences over 1/64 CSS pixel, removing created nodes and restoring
the caller's measurement hook on rejection.

Nested construction includes block flow with uniform nonnegative collapsed margins
(no outer collapse), vertical stretch/fill and intrinsic blocks in wrapping rows.
Linked Form, block and configured-font Toolbar fixtures cover both themes at 320/1280px,
history, isolation and two saves. Private copy keeps its owner's properties.
Toolbar also composes with linked Text in named header/body slots backed by Stack:
nested copy edits resize the page body without changing the sibling's copy or size
through two saves. Browser checks retain the heading and keyboard-focusable link.
This synthetic page body has no navigation chrome, artwork or prototype transitions.
Configured-font Card pages also compose Heading, Stack and Grid; title/description
edits retain linked ownership and content-driven grid height after two saves.
Single literal zero-spread box shadows retain native effects. Straight-edge samples
match Chromium within one channel value for opaque, translucent and transparent
cards in both themes at 320/1280px. Inset, multiple, spread and token-dependent
shadows remain refusals; artwork, hover presentation and rounded-edge pixels are unverified.
Nonwrapping intrinsic rows and submission are not modeled; default display-font fidelity is unverified.
Input values stay unwrapped; fixed-row Textarea values wrap inside a clipping viewport.
LFs, blank lines, clearing and measured edits survive two saves; failures roll back history.
Text-property fields accept Enter for newlines and Ctrl+Enter to commit.
Caret scrolling, scrollbars, manual resizing and textarea controllers are not modeled.
Single-line labels retain required markers after edits and saves; only label/value bind. Multiline labels remain unverified.
Text binds `content` inside its wrapping block. Light/dark 320px and 1280px checks
cover line breaks/advances, property history and two saves. The editor test edits
and downloads the 320px paragraph. Reopened direct Text instances also pass width
reflow; arbitrary container-resize history remains unverified.
Captured horizontal Flex rows wrap linked children with CSS minimum gaps and
start/center/end/space-between alignment. Constructor-based checks cover both themes,
320/390/1280px reflow, label history, isolation and two saves. A separate linked-box
editor fixture verifies keyboard resizing, history and two downloads; fixed-size
ownership survives synchronization while HUG dimensions reflow. Reverse/column
wrapping, reordering and other line alignments remain refused. Run `npm run test:browser`.

[bindComponentProperties](bindings.mjs) preflights exact constructed handles before
binding source strings to native `TEXT` properties and the supported single-icon
occurrences to `INSTANCE_SWAP`. This restricted projection does not redefine Go
content slots as single replacements. Native definitions and references drive behavior.
Provenance records the source invocation, font identities, viewport, observed
environment and a versioned native-property-ID to source-field map. Native display
names can change without changing that map; provenance is not authentication.
Explicit slot-property maps distinguish icon assets from nested source components;
asset instances do not acquire string-proposal ownership by being inside a slot.

[bindComponentVariants](bindings.mjs) groups already materialized source projections
as `bindComponentVariants(graph, emptySet, baseSnapshot, exampleId, property, variants)`;
each variant supplies `{ snapshot, master }`, including the exact baseline export.
Obtain candidate snapshots through `--proposal`, then use the existing capture and
materialization path. The set owns shared text definitions and one native variant
definition; children retain their projected source revisions. Exact values, including
empty strings, come from source properties, never visible labels or layer names.
This currently supports one unconstrained string on a nonopaque leaf component.
Go Button tone projections verify shared-copy sizing, colour, history and two saves
in both themes. Catalog enumeration, composed families and native Select conversion remain unfinished.

[associateSourceInstance](source-changes.mjs) maps an exact `graph.createInstance`
result to one source root after validating its subtree. Previews stay unmapped;
copied correspondence is refused. Child templates carry relative IDs and slots,
not absolute placement paths. Masters identify their source with `definitionPath`;
older definitions without it require regeneration for replacement extraction.
Both extractors take an exact instance and canonical snapshot, deriving its
destination path through persisted native lineage. `extractSourceProps` returns
`{baseSHA256, path, props}` for mapped, unconstrained strings. Native definitions,
bindings, baselines, assignments and rendered text must agree. Validate the result
with [ui.ProjectProps](../../../ui/proposal.go).

After a native swap, `extractSourceReplacement` returns
`{baseSHA256, path, replacementPath}`, identifying root or nested source occurrences.
It refuses ambiguous, stale or missing correspondence and additional bound-string
edits inside the replacement subtree. This is intent, not acceptance:
[ui.ProjectReplacement](../../../ui/replacement.go) owns interface compatibility,
observed source ownership and freshness checks; the adapter does not duplicate them.
Run Core's `go run ./tools/designexport --proposal` or `--replacement` with the
matching JSON on stdin; it returns a candidate snapshot without saving source.
Extraction changes neither graph nor snapshot. Refusals include status, code and
explanation; `no-supported-changes` refers only to the selected capability, not
whole-scene equivalence. Persistent revision checks remain the caller's responsibility.

Browser comparisons use Go Button with 16px leading and 20px trailing icons in both
themes, IBM Plex Sans 600 and `--font-render-hinting=none`. Label edits, icon swaps,
undo/redo and two saves retain links and match dimensions and icon placement within
1/64 pixel. Fresh derived-layout records retain sibling positions after text grows.
CanvasKit's shaped advances remain unchanged; source text-row layout applies
[Chromium's intrinsic-box rounding](https://raw.githubusercontent.com/chromium/chromium/782af9cb30a53f54487e5d2e44738645a8ec457c/third_party/blink/renderer/platform/fonts/shaping/shape_result.h)
before positioning siblings. Ordinary native text keeps its own layout behavior.
These checks do not establish every platform's hinting, glyph-baseline/pixel
equivalence or native accessibility. Separate palette tests verify painted pixels
after each save; page-mode serialization and imported-instance indexing retain
native modes and live links.

Icon composition verifies canonical flat path/circle geometry, attributes and
observed paints; grouped, transformed or potentially clipped SVGs are refused.
Arbitrary nested content, nonuniform or nonsolid borders, outlines, filters,
truncation and unimplemented sizing constraints remain unsupported. Text controls
support empty values; text blocks and flex labels still need empty/collapsed-space
participation semantics. Native properties accept those edits, but the converter
refuses such initial text blocks; property identity is not source-layout fidelity.
This development boundary is not a complete editable component library.

Native creation, updates and replacement retain FIG's authored `uniformScaleFactor`.
Flat, solid-painted vector masters scale from FIG-representable canonical geometry,
including strokes, without detaching or cumulative resizing. Tests cover 20px and
16px instances, composition, master updates, factor edits and clearing.
Page-placed owners, including two nested scopes, pass actual browser icon swaps,
undo/redo and two worker saves without changing siblings or masters. Unscaled
fixtures retain dimensions and exact slot GUIDs; containing-master updates preserve
replacement inputs. Failed history preflight retains its entry without graph events.
Locally edited descendants remain refused; exact subtree/layout history is unfinished.
The generated Form's direct swap and extracted proposal, with caller-run layout,
match Go replacement geometry in both themes through two saves.

Stroke caps and joins survive serialization; conflicting styles and nonrepresentable
geometry are refused. Flat vector replacements retain consistent source-occurrence
color-variable mappings through history and two saves. Correspondence uses native
variable identities, not names or vector positions; ambiguous, partial or missing
roles are refused. Literal-only vectors retain their inheritance. Swapped bindings
remain live native overrides, but later source-role reassignment does not retarget them.
These checks do not establish scaling for text, effects, dashed vectors, arbitrary
nested masters or editor drag handles. Source icon-slot conversion retains its stated limits.

[Synchronization](sync-correction.mjs) plans and validates a projected result before
applying changes through native graph APIs. Planning allocates no native IDs and
emits no graph events; SDK copy helpers are reused. Geometry calculation and source
identity reconciliation contain no product or catalog rules. The effect boundary
owns ID allocation, notifications and temporary deletion ancestry, so a missing
reference is never treated as evidence of a deletion.
Nested instances retain their own root and descendant overrides during a
containing-component synchronization; explicit outer paths retain precedence.
Variants in one component set inherit shared TEXT definitions by native property
ID. Switching retains each bound text node and its compatible layout ancestry,
so local copy edits and their undo history survive reordered, differently named
variant layers and FIG saves. Missing targets, duplicate definitions and ambiguous
ancestry merges or depth changes refuse before writes. Unbound edited descendants
still require subtree history; this is not general cross-component replacement or
source Select conversion. Nested instances resolve variant values and switching
through the same canonical lineage as their shared text properties.
Browser checks exercise label editing, keyboard variant
switching, undo/redo and three worker saves without changing masters or siblings.

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

The [editor check](editor/replacement.test.mjs) refuses stale builds and tests generated
Form/Button/Text properties and icon swaps through history and two downloaded saves.
Generated secondary Text also retains its authored colour role through keyboard palette edits,
undo/redo and worker saves; native tests cover both themes and translucent border pixels.
Fresh contexts check links, proposals, masters, siblings and placement positions.
Font settings loads process-only OTF fixtures with verified digests. Source comparisons
reuse the generation browser; full Chromium runs the editor. CI treats its isolated
HTTP origin as secure. Saves load unopened pages without replay; tree navigation causes
no canvas nudges. OS pickers, hardware GPUs and full accessibility remain unverified.
No document, WebGPU assets or product fonts are packaged; PWA registration is disabled.

Native properties and FIG override paths retain links through reflow and undo;
repeated saves merge overrides by path. Low-level property fixtures use a
deterministic measurer, while component comparisons use the supplied real fonts.

[Native variant history](variant-correction.mjs) retains set definitions, child
values and ID-based FIG specifications together. Empty defaults are explicit;
rename/remove undo restores owned metadata without reverting unrelated appearance.
Invalid names and stale history refuse before mutation; failed writes restore the
previous state and leave history retryable. Run
`node --import ./register.mjs --test variant.test.mjs` for fresh/imported graphs,
exact values and two-save checks. The editor check drives the shared choice control
by keyboard, preserving empty and reserved-looking values through three worker saves;
mixed selection is separate UI state, not a reserved source string. This is an SDK
prerequisite, not source Select conversion; the source-family API above has narrower scope.
Variant switching and its history stage node changes and property layout before
commit. Shared text inherits the selected master's typography, including line height;
explicit occurrence overrides remain local. Tone and size families are checked through editor saves.
The existing synchronization plan applies these changes. Synchronous measurement failures publish no native
mutations and leave history retryable. Staging currently copies the node map;
large-document latency and memory remain unverified, as do arbitrary commit-listener failures.
Bound paints keep their authored fallback RGBA on import instead of acquiring default-mode
palette colours as paint edits. Solid and dashed strokes multiply resolved alpha by opacity.
Normal graph subscriptions and history are checked after deferred notifications
settle. Property operations pause synchronization only while computing/restoring
captured geometry; authored parent resizing still propagates master dimensions.
Failed measurement leaves grid sizing modes unchanged and frees the built Yoga trees.
Grid and flex now share recursive native measurement. Low-level tests cover HUG and
fixed row heights, nested wrapping text, spans, padding, hidden/absolute children and
explicit stretch. FR tracks retain their content minimum; shrinking below it needs
an explicit minimum-size choice.

[Native grid persistence](grid-fig-correction.mjs) writes FIG's own ordered track
GUIDs, sizing maps, gaps and cell anchors, without a second document format.
FIXED/FR/AUTO tracks, automatic rows, explicit spans and tested cell FILL/HUG axes
survive two saves. Nested instances retain local track, gap and placement ownership;
untouched fields still inherit. Keyboard track edits, undo/redo and two browser
worker saves preserve measured cell geometry without changing masters or siblings.
Native grid measurement invalidates its participating subtree's imported box cache;
failed measurement restores that cache, and unopened-page population preserves edits.

The corrected native track retains optional `minValue`, a finite nonnegative fixed
minimum mapped directly to Yoga and FIG's existing min/max fields. This preserves
CSS `minmax(0, 1fr)` without changing the cell's own minimum-size contract. Automatic
placement uses native automatic start lines, including cells with spans.

[Source Grid planning](source-grid.mjs) consumes captured CSS Typed OM track values,
not the browser's resolved pixel tracks. It feeds the existing composition builder;
source-owned Text and Button cells remain linked instances. Two- and three-column fixtures in
both themes compare 320px, 390px and 1280px layouts, full-width spans, unequal-height
content, local text-property history and two saves against Go/Chromium output. Definitions retain
intrinsic sizing; placed instances receive their parent's stretched dimensions.
Button definitions match separately captured standalone controls, while their placed
labels retain centering, wrapping after edits and CSS advance rounding. Source tests
also exercise the shared fill-restoration path in stretched vertical Flex layouts and
check keyboard order, accessible names, focus indication and Enter/Space activation;
screen-reader, focus-contrast and complete accessibility coverage remain unverified.
The default gallery Grid now contains three captured Text cells instead of one
anonymous text run. Track parsing supports bounded integer repeat, fixed, fractional
and automatic tracks, and min/max with a fixed minimum.

The boundary refuses malformed tracks, foreign anchors, unsupported min/max sizing,
automatic columns, unrepresentable leaf alignment and instance layout-mode replacement.
Source conversion additionally refuses automatic repeat, named areas/lines, reordered
cells, unsupported alignment and explicit cell minima. Track/gap edits are not source
property proposals. The grid sizing menu still only exposes fixed dimensions; complete
controls, broader CSS layouts and arbitrary imports remain unfinished. These checks
do not establish a release-ready component library.

Text-property guarantees cover placed root or nested targets and exact layout undo/redo.
Master-owned edits, variants and arbitrary imports remain unverified; identity
must be unambiguous. These tests are not a universal replacement contract.
The reordered-instance tests prove that labels retain the correct identity;
they do not prove persistence of per-instance child order. The SDK currently
rehydrates children in master order.

## Release blockers

The upstream lock audit still fails; the adapter audit does not cover that tree.
Missing package notices are supplied under `/licenses/` from Dockerfile-pinned sources.
Build tools, copied assets, fonts, SVG fidelity, responsive/accessibility and source conversion remain
incomplete. Product prototypes need runtime-state mappings
and governed end-to-end persistence tests in their owning product repository.
Native sizing still needs evidence beyond the declared comparison cases, including
overflowing and whitespace-only flex labels during edits. Correct font loading
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
