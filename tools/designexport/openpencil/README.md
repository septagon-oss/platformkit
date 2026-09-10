# OpenPencil adapter tooling

This directory owns the native boundary of the existing
[design export](../README.md). The Go tokens, glyphs,
typed examples and stylesheet remain the source of truth. There is no second
component registry, page language or client-specific library here.

The generator packages tokens, icons and explicitly selected experimental native
components, using supplied-font validation, browser observations and a pinned
SDK correction layer. Product pages and flows are not converted yet; this is not a
complete published component library or the finished shared provider interface.
The [coverage test](browser/text.test.mjs) reports each admitted or refused example
with its reason, using IBM Plex Sans 400/500/600/700, headless Chromium
(`--font-render-hinting=none`), light mode and a 1280×900 viewport. Run
`npm run test:browser` for the current inventory. These measured checks do not
establish complete typography, visual, interaction or provider support.

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

For typed tokens, the producer first selects a dependency-closed `ui.TokenExport`
and attaches it with `DesignExport.WithTokens`. Pipe that v2 snapshot into the same
`--snapshot-stdin` command without component options. The adapter admits exactly
`source-tokens.v1`: selected light/dark colours, native aliases, premultiplied sRGB
blends, `px` scale values and explicitly unitless leading, line-height or font weight.
It validates every requested feature, token dependency and selected icon before
allocating native IDs. Scale-only selections may omit icons and theme modes.

Ordered fallback fonts, contextual units, keywords, shadows, motion and selected
asset/face records refuse the whole request. Assets still use the separate
[font-delivery bridge](#deliver-source-backed-font-assets). The full default
`designexport --tokens both` selection therefore does **not** produce a complete
native library. A producer must select its subset explicitly; this adapter neither
filters one automatically nor downgrades a v2 request to a v1 consumer.

JavaScript callers use `decodeSnapshot(bytes)` from [source-tokens.mjs](source-tokens.mjs),
then `buildFoundation(decoded.snapshot, decoded)`. The bounded UTF-8 decoder returns
ordinary snapshot data and identity-addressed `scalarSpellings` records
(`{scale, key, decimal}`), rejecting duplicate JSON fields.
Keep that pair together: `JSON.parse` alone has already lost authored decimal spelling.
The pure `planSourceTokens(snapshot, scalarSpellings)` checks source selection and
conversion. `buildFoundation` also exercises the pinned native resolver on detached
records before allocating IDs, including its expression limits. These checks do not
authenticate supplied hashes or replace the Go producer's source contract validation.

[Token metadata](variable-source.mjs) retains the captured source identity, snapshot,
unit and original scalar decimal independently of native names and edited values.
It is baseline evidence, not source-write authority or acceptance of a native edit by
the Go source contract. Colour formulas reuse the existing evaluator and linked icons.
Qualified native pixel consumers are described below; source-produced components
and layout still use their v1 observation path. Numeric storage alone does not
establish source-token-to-component correspondence or typography bindings.

To include components, install Chromium as described below and append repeated
`--example ID` selections and `--font FAMILY WEIGHT STYLE /absolute/font.woff`
arguments. Quote family names containing spaces; supply every required static face
with its actual family, weight and style. No brand font is selected implicitly.
For example, `--example pk-ui.component.form/default` selects the linked Form.
Add repeated `--variant ID PROPERTY /absolute/projection.json` for nonbaseline
`ui.ProjectProps` snapshots (32 MiB each); the ID must also be selected. These
caller-supplied projections are validated for correspondence, not source freshness.
For a nested leaf, use `--variant-at '["pk-ui.component.form/default","actions","create"]' size /absolute/large.json`;
the projected snapshot must change that exact invocation, and the root must be selected.
This builds a child family inside the existing composition, not copies of the whole Form.
Optional `--mode light|dark` and `--viewport WIDTHxHEIGHT` choose one observation
profile; defaults are light and 1280×900. Every requested example must pass or no
file is created. Construction and source correspondence are checked across two
saves. Ordered layout is repeatable; native IDs and FIG bytes are not.

For the default v1 output, inspect Foundation: 22 colors, three CSS font-family strings,
27 icon masters and 54 mode-bound light/dark previews. These strings are not
installed fonts or native text styles. Component definitions and Editable source
instances occupy separate pages; edit the latter's native properties. Each root
has one source correspondence, not duplicate theme or responsive claims. FIG
retains font identities and glyph outlines, not font files: editing requires the
same fonts separately in the editor. No page prototypes are included.

[buildComponentDocument](document.mjs) accepts one existing `ui.Export` snapshot,
explicit `examples`, `fonts`, `mode`, `viewport`, and caller-owned `browser` and
`renderer`, plus optional `variants: [{ exampleId, path?, property, snapshot }]`.
`path` defaults to `[exampleId]`; nested paths use exact source invocation IDs.
Family selections expose `family` as property owner, inherited `properties` and
all state masters in `components`; `master` and `instance` retain the baseline.
`families` lists `{ path, family, properties }` for every requested leaf, including nested ones.
It returns foundation and definition/placement handles. [buildFoundation](foundation.mjs) supplies the shared
`{ graph, collection, icons }`; `prepareIcon` validates glyphs without creating nodes.

Legacy v1 inputs are the existing light/dark token contract, literal hexadecimal
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

[Variable and collection deletion history](variable-history.mjs) retains detached
values, source records, collection order, active selection and affected bindings
and instance override flags. Undo validates the complete restoration before any
live write or notification; lost dependencies, reused identities and conflicting
bindings refuse without consuming history. Unrelated geometry, variables and
override fields remain intact. Redo records its current deletion effects rather
than sharing a mutable snapshot with the graph. Numeric and colour aliases use
their existing validators, including plain colour dependencies. Linked icon
bindings survive restoration and two actual editor worker saves. The undo stack
itself is session state, not FIG content. Raw graph mutation, arbitrary event
callbacks and unrestricted formula creation remain outside this qualification.

[Numeric variables](variable-number.mjs) retain the editor's finite binary64 values
alongside ordinary binary32 FIG fallbacks. Versioned metadata preserves decimals,
tiny values and signed zero through two saves; numeric aliases remain native aliases.
Import refuses incomplete or changed metadata and incompatible fallbacks. Other
editors may discard this extension, and changes within one binary32 rounding interval
cannot be detected. An old file without metadata retains only its existing fallback
precision. This is not preservation of an authored source-token decimal spelling.

Value cells use labelled native inputs: Tab or Enter commits, Escape cancels a draft,
and a refused edit restores the committed value without consuming undo/redo history.
Number input requires a complete decimal representable by the editor without decimal
rounding and with a finite FIG fallback; `12px`, overflow and underflow below binary64
are refused. Native tests cover aliases, mode fallbacks and metadata integrity. The
built-editor numeric test additionally covers keyboard focus, visible outlines,
refusal feedback, history and two actual worker saves without changing component
geometry. Live screen-reader announcements and a full accessibility audit remain
unverified. Storage precision and layout consumption have separate checks.

[Native pixel bindings](variable-binding.mjs) resolve flex gaps, four padding edges,
uniform rectangular radii and explicitly fixed rectangular box dimensions through
the existing node-aware resolver. Aliases retain their references; inherited and
local modes select each occurrence's value. Source-backed dependencies must be
absolute pixels. Negative values, incompatible units, intrinsic/fill-owned sizes,
conflicting size limits and independent corners refuse before binding. Yoga and
FIG geometry still use binary32 precision; the variable's binary64 value and its
source decimal are separate records, not promises of exact layout arithmetic.

Ordinary editor value, binding, node-mode and collection-mode actions stage numeric
and layout effects through the shared detached graph projection. Measurement or
validation failure leaves live values, geometry, events and retryable history
unchanged. Derived changes do not acquire literal instance overrides. Explicit
unbind freezes the effective value as an authored literal; rebind releases it.
Collection removal and restoration preserve distinct occurrence values and links.
Editing a bound numeric literal requires an explicit unbind first.

Run `node --import ./register.mjs --test variable-binding.test.mjs` here for live
reflow, both flex directions, fixed boxes, alias and mode ownership, Skia pixels,
failure injection, history, instance independence and two native saves. The editor
check below additionally edits a source-identified pixel token by keyboard, reads
live linked dimensions, rejects a negative value and checks history and two worker
saves. Its token-dialog audit covers names, contrast, focus and refusal feedback;
actual screen-reader announcements remain unverified.

This is a provider consumer boundary, not a second source token map. It does not
infer references from equal pixel values or migrate the component exporter to v2.
Typography, contextual units, grid tracks, motion and shared-style bindings remain
outside this qualification, as do raw graph writes and arbitrary event callbacks.
The existing enum-contract migration and complete generated-library gates are
separate unfinished work; these focused checks do not establish production readiness.

[Collection mode edits](variable-modes.mjs) run the pinned graph operations on a
detached owner, validating the resulting numeric and colour dependencies before
live writes. Add, duplicate, remove and default-mode history retain captured values,
including absent values; stale or dependency-breaking operations refuse without
consuming history. Undo preserves an independently changed active-mode selection.
The native wire order selects the default, while `sortPosition` preserves visible
column order, so a nonfirst default survives both saves without another metadata
format. Legacy mode lists without positions retain their original order; mixed or
duplicate positions refuse. Active-mode selection itself remains session state.

Mode headers are keyboard-operable menu buttons, and mode changes update the
existing scene-derived table. Value-only edits retain its cell renderers and focus.
Tabbing across a short token name no longer commits a rename; intentional leaf
renames preserve its group prefix. Inherited numeric values display their resolved
value with an accessible explanation, without materializing an override on focus.

Collection renaming keeps the selected tab and its panel label mounted. A separate
labelled input opens after the collection menu releases focus; Enter commits and
Escape cancels only the rename, returning focus to collection actions. Keyboard
history and two worker saves retain token source identities and linked icons.
The existing double-click entry remains available; blur commits retain the browser's
chosen Tab destination. The table's actions column has an accessible heading.

The typed-token table uses the editor's existing readable foreground role for
headings, values and instructions; this does not change source palette tokens.
Its test-only axe check covers loaded, renamed, refused-edit, filtered,
empty-result and empty-collection states using the source-produced fixture.
Empty results retain the table's column structure and describe the search field.
Colour swatches name their variable and mode. RGB, HSL and HSB controls have
named fields and a format selector; slider labels fit beside their tracks. The
shared popup primitive exposes its controls reference only while open. Keyboard
edits in both modes retain source identities, aliases, linked icon structure,
history and two worker saves. The table and these picker states require a clean
axe result, including no inconclusive checks; keyboard focus is also checked
with normal and forced colours. Existing parsers still own colour conversion.
Browser colour preferences do not change the editor's dark interface. Wider
editor accessibility, OkHCL controls, zoom and live screen readers remain
unverified; these bounded checks are not an editor-wide accessibility sign-off.

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

Browser files run with two workers; each owns Chromium and native rendering.
This bounds test resource use independently of the host CPU count.

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
not per-option layout. Closed single Select composes a real native control with the
shared grid and canonical chevron; the decoration ignores pointer events. Native
construction uses that linked glyph and the observed display viewport. Stored `value`
is a finite source-projected variant, never the visible option label's TEXT binding.
Use `--variant` or `--variant-at` with `value` projections to make choices editable;
without projections, only supported literal fields such as `label` are exposed.
Exact and empty values, disabled/error states, nested Form ownership, keyboard focus,
forced colors, history and two saves have bounded tests. Multiple/listbox selection,
duplicate values, competing `values`, unmatched browser defaults and UA chrome refuse.
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
one planner for solid fills, same-paint solid or uniform dashed borders, radii and padding, retaining
transparent token-bound strokes and border insets. Direct aliases require matching palettes in every theme
and retain source RGBA through legacy CSS alpha rounding. This is candidate evidence,
not general CSS equivalence. Literal paints stay unbound. Unambiguous authored expressions
become COLOR variables in the existing foundation collection, keyed by their source CSS
custom-property names and bound to native input IDs. Matching roles are reused; conflicting
or edited roles refuse without being overwritten. Allocation follows successful construction
and geometry checks, and failed construction removes its own nodes and variables.
Supported text, fills, borders and canonical SVG currentColor occurrences retain
these formulas through palette edits, history and two saves. Glyph swaps preserve occurrence
colour roles without changing canonical assets. Ambiguous, stale and unsupported expressions
remain refusals; this does not add layout capabilities or certify arbitrary CSS.
Construction rejects geometry differences over 1/64 CSS pixel, removing created
nodes and restoring the caller's measurement hook. Measurement failures return
through Yoga before being rethrown, keeping its WebAssembly state reusable.

Observed constructor-internal captures retain linked masters without becoming
replaceable slots. Their derived property values must remain at the source
baseline, including beneath further nested components. Source extraction offers
no independent property edits, replacements or variants for those paths; changing
derived text refuses a proposal rather than concealing it behind a sibling edit.
Declared slots outside that boundary remain editable. This does not infer arbitrary
Go dependencies or make native edits execute the source constructor.
Parent-owned stretch sizing survives variant changes and FIG reloads without
changing the reusable definition's intrinsic sizing.

Nested construction includes block flow with uniform nonnegative collapsed margins
(no outer collapse), vertical stretch/fill and intrinsic blocks in wrapping rows.
Border-box paragraphs support pixel maximum widths and intrinsic flex-column alignment.
Configured-font EmptyState retains linked actions, nonempty copy edits and responsive line boxes through two editor saves.
Nonnegative flex margins use private margin-box frames; fixed border-box sizes and
zero-basis horizontal fill keep reusable children independent of their placement.
Trailing fixed flex boxes and nonempty auto-sized text badges use parent-owned edge anchors;
width-led aspect ratios, padding-edge hidden overflow and tabular numbers retain source layout through two saves.
Stretch insets, stacking and transformed/scrolling containing blocks remain refused; artwork conversion is unfinished.
Generated refusal forms inherit Alert text, icon offsets and rounded independent
border widths through edits and two saves at 320/1280px in both themes. Text inputs
also require an observed browser editing viewport, not inferred content offsets.
Linked Form, block and configured-font Toolbar/Stack/Text fixtures cover both themes at
320/1280px, copy edits, isolation, history and two saves. Private copy keeps its owner's properties.
Toolbar's named header/body slots retain heading semantics and a keyboard-focusable link.
This synthetic page has no navigation chrome, artwork or prototype transitions.
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
Text binds `content` to one editable paragraph. [Source wrapping](paragraph-correction.mjs)
keeps oversized words intact; tests cover alignment, Unicode offsets, selection, painting,
property history and two saves. Controls and unmarked native text retain their own policy.
Hyphenation, dictionary-based breaking and paragraph container-resize history remain unverified.
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
as `bindComponentVariants(graph, emptySet, baseSnapshot, pathOrExampleId, property, variants)`;
each variant supplies `{ snapshot, master }`, including the exact baseline export.
Obtain candidate snapshots through `--proposal`, then use the existing capture and
materialization path. The set owns shared text definitions and one native variant
definition; children retain their projected source revisions. Exact values, including
empty strings, come from source properties, never visible labels or layer names.
This currently supports one unconstrained string on a nonopaque leaf component.
Go Button tone and size projections verify shared-copy sizing, colour, history and
two saves, including Form → FormActions → Button in both themes at 320/1280px.
Ancestor HTML, sibling contracts and source byte spans must remain exact around the
selected child. Select value families use the same path. Catalog enumeration and composite variants remain unfinished.

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
Arbitrary nested content, differently painted or unsupported border styles, outlines, filters,
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
still require subtree history; this is not general cross-component replacement.
Private assets are compared with their owning source occurrence, including inherited
scale and paint. Automatic layout positions do not claim authored edits; FIG retains
size dirtiness. Nested instances resolve variant values and switching
through the same canonical lineage as their shared text properties.
Browser checks exercise label editing, keyboard variant
switching, undo/redo and three worker saves without changing masters or siblings.
Size-only switches are tested without a preceding label edit: inherited changes
invalidate stale imported dimensions and layout fields before FIG serialization.
Untouched source encodings remain intact, and undo restores their edit markers.
Native checks cover standalone and nested variants with custom fixed width and
right padding through two saves and subsequent synchronization; the browser
checks compare unedited-label Button sizes against fresh Go projections.

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

### Deliver source-backed font assets

[editor-fonts.mjs](editor-fonts.mjs) also accepts a caller-owned delivery manifest
through the existing builder. In a disposable `build-inputs` container, supply
an absolute manifest path and its package directory, then run:

```sh
node /adapter/build-editor.mjs /upstream --font-assets /owned/fonts.json
```

This writes the static editor to `/upstream/dist`; it does not publish or deploy.
The UTF-8 JSON object has exactly `schema`, `assets`, `faces` and `files` fields.
Use schema `platformkit.font-delivery.v1`. `assets` and `faces` are selected
[Core Asset and FontFace records](../../../design/assets.go), unchanged from
their source projection. Each `files` entry is `{ "asset": "selected-asset-id",
"font": "fonts/regular.woff", "notice": "licenses/OFL.txt" }`.
Every selected asset needs a face and exactly one binding; unknown fields,
duplicate identities and unselected bindings are refused.

Bindings are portable relative paths beneath the manifest directory. Absolute
paths, traversal and symlinks escaping that directory are refused before asset
reads. Keep that caller-owned package immutable for the build. Provenance
`source` fields are evidence, never download instructions. The bundler verifies
the declared asset and individual notice digests, nonblank UTF-8 notices,
actual static TTF/OTF/WOFF format, family, style, weight and PostScript name.
This provider requires literal integer weights 100–900 in steps of 100; Core's
wider numeric domain does not imply editor support.

All font preflight completes before build output. Validated bytes extend the
same SDK bundled loader and picker; stock faces cannot be replaced. Equal files
share content-addressed paths, while each provenance face retains `sourceAsset`
and `sourceFace` alongside its physical hashes and notice path. These inputs
cannot be mixed with the legacy `--font`/`--font-license` preview arguments.
The default Docker target remains unchanged. This boundary proves selected
font delivery, not glyph coverage for arbitrary text, a native token library,
application font hosting or permission to redistribute the supplied assets.

## Understand the correction boundary

[CSS dashed borders](border-correction.mjs) fit dashes to resized edges, with uniform
circular corners, one shared paint and a nonempty inner box. Their interpretation
lives in the existing source correspondence; unrelated native borders are unchanged.
Changing a native pattern, width or unsupported shape releases that interpretation.
Native inheritance and explicit dash overrides survive two saves. Browser checks cover
square, rounded and pill shapes, thin/thick strokes and transparency; they compare
paint and dash placement, independently calibrate rounded-clip precision and classify
partially covered antialiased edges. This is not byte-identical rasterization across
Skia versions. Displaced, missing, fixed-pattern and recolored borders fail the check.
Go-backed linked examples cover token ownership, copy edits, history and two saves.

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
Form/Button/Text and nested Select properties and icon swaps through history and two downloaded saves.
Generated secondary Text also retains its authored colour role through keyboard palette edits,
undo/redo and worker saves; native tests cover both themes and translucent border pixels.
Fresh contexts check links, proposals, masters, siblings and placement positions.
Font settings loads process-only OTF fixtures with verified digests. Source comparisons
reuse the generation browser; full Chromium runs the editor. CI treats its isolated
HTTP origin as secure. Saves load unopened pages without replay; tree navigation causes
no canvas nudges. OS pickers, hardware GPUs and full accessibility remain unverified.
No document, WebGPU assets or product fonts are packaged; PWA registration is disabled.

The Dockerfile's explicit `--target preview` adds the four licensed IBM Plex Sans
verification faces through the SDK's existing bundled-font loader and picker.
They load from the same editor origin, including tailnet HTTP, without local-font
permission or desktop-only providers. The default `editor` target stays generic.
The builder also accepts repeated `--font FAMILY WEIGHT STYLE /absolute/file`
and one `--font-license /absolute/notice` for another caller-owned static profile.
Shared font validation rejects false identities and duplicate faces; packaging
refuses replacement of SDK faces. `platformkit-provenance.json` records supplied
face hashes, asset paths and the included license. This is font delivery, not a
Collect typography decision or evidence that another profile matches its source.
Against a disposable `preview` target, run
`PLATFORMKIT_OPENPENCIL_URL=http://127.0.0.1:18090 node --import ./register.mjs --test preview/*.test.mjs`.
This checks automatic font loading without local permission, visible button/field
glyphs and alignment, exact font digests and two browser worker saves/reopens.
Pixel comparison waits for every referenced face and excludes the overlapping editor toolbar.
FIG export still needs a secure browser context for WebCrypto; tailnet HTTP is
not sufficient for saving. Use HTTPS for shared previews. CI marks only its
isolated service origin secure, as in the generic editor check.

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
mixed selection is separate UI state, not a reserved source string. The source-family
API above has narrower scope than the underlying SDK choice controls.
Variant switching and its history stage node changes and property layout before
commit. Shared text inherits the selected master's typography, including line height;
explicit occurrence overrides remain local. Tone and size families are checked through editor saves.
Nested replay also restores retained occurrence links, names and source override anchors;
it does not promise stable identities for unbound decorative layers replaced by a swap.
FIG export retains local nested layer names as occurrence overrides, leaving definitions and sibling placements unchanged.
The editor's option list resolves the same canonical component lineage as property values and switching; nesting does not create a second interface.
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

The browser's AI provider imports use `@ai-sdk/provider-utils@4.0.33` through
the existing direct-dependency boundary. The upstream UI lock alone selects
older response readers affected by [unbounded response bodies](https://github.com/advisories/GHSA-866g-f22w-33x8),
which an adapter-only audit could not see. JSON success, JSON error and status
error tests require ordinary responses to work and oversized bodies to cancel
at the header or first over-limit chunk. Built-editor checks require the patched
version in the emitted dependency report. The upstream default is 2 GiB, not a
browser-appropriate memory budget; a smaller application limit remains undefined.
These checks exclude binary and streaming-event readers. This does not clear the separate
upstream lock audit or establish complete browser dependency reachability.

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
