# Pure SVG values

This standard-library-only package owns the immutable compiler, geometry grammar
and XML encoder shared by composed pictures and finite-choice figures. It opens
no files, URLs, transactions or servers. `modules/picture/contracts` retains
compatible aliases and delegates for composed pictures; access-controlled
picture delivery remains with that module.

## Composition

**Reused** — the picture module's bounded compiler, numeric/path grammar,
immutable declarations, canonical values and XML encoder.
**Added** — a figure profile and verified JSON value because composed pictures
select one option per part and cannot describe repeated interactive regions.
**Made reusable** — one pure SVG implementation for value metadata and image delivery.

## Figures

`CompileFigure(name, source, ids)` accepts 1–256 unique IDs matching
`[a-z][a-z0-9_-]{0,31}`. Every ID must occur on a non-root element's `data-choice`;
no other ID is allowed. Several disjoint elements may name the same ID, while
nested markers refuse. Unmarked decoration and children of marked groups are
allowed. Supply only ordinary IDs: an exclusive “No preference” option has no region.

The [composed SVG grammar](../../modules/picture/README.md#templates) and its
bounds apply, with these changes:

- Replace `data-part`, `data-option` and `data-slot` with `data-choice`.
- Admit leaf `text` with optional scalar `x`/`y`, positive `font-size` at most
  256, `text-anchor` start/middle/end and `font-weight` normal/bold/100–900 in
  steps of 100. Text is at most 1024 decoded UTF-8 bytes per element and 16384
  total. Font family is inherited; authored font loading is unavailable.
- XML predefined and numeric entities become escaped text. Source IDs/classes,
  styles, handlers, scripts, foreignObject, links, use, images, URL references,
  gradients, masks, animation, authored ARIA and tabindex remain refused.

Bounds remain 262144 source bytes, 2048 elements, depth 32, 20 attributes per
element, 65536 bytes per attribute, 16384 numeric tokens and 1 MiB output.
Errors return no usable declaration or partial output. Diagnostics are bounded
to 256 bytes. The composed profile still refuses text and retains its exact
source digest and output identity.

`Figure.SVG()` is neutral canonical XML, with sorted attributes and explicit
end tags. `Digest()` is its SHA256; discarded comments do not affect it.
`ChoiceIDs()` returns a detached sorted slice. `Render(selected)` accepts a
unique subset, including empty, and sets `data-selected="true"` on every selected
region without changing the figure. Unknown or duplicate selections refuse.
The UI supplies token-based highlighting; the compiler embeds no CSS.

JSON contains exactly `digest`, `svg`, and `choiceIDs`. Unmarshal rejects duplicate
or unknown keys, recompiles the SVG and compares all canonical members before
replacing the receiver. A zero figure cannot render or serialize. This value
can be copied and used concurrently throughout the declaration's lifetime.

## Limits

Figures are public product artwork, not uploads or access grants. Products own
vocabulary and artwork. Stored option pictures still require their authenticated
image delivery. This package supplies neither selection rules nor a browser controller.
