# Composed pictures

Picture serves a declared inert SVG template and a complete value as a private,
authorized image. It has no upload path, table, mutation, page, or public route.
Applications import [contracts](contracts/), declare artwork with `Compile`, and
provide `contracts.Source` and `contracts.Templates` through an ordinary typed
pkit provider before composing `picture.Module`.

## Composition

**Reused** — `pkit.NewModule`, `httpx` App admission and tenant transactions,
`cache.Group` through its tenant-key `Pool` facade, standard XML serialization,
and the existing entity/rest/screens/resource image path and `components.Media`.
**Added** — a constrained template/value decision, source-authorized image door,
and derived `entity.Picture` reference because uploaded files and PDF templates
cannot validate and deliver computed inert SVG without changing their contracts.
**Made reusable** — immutable compiled declarations, canonical render identities,
`picturetest.RunDecisions`, a domain-free SVG fixture, `cache.Pool`, and the
request-time `httpx.CacheFrom` seam for other lazy composed values.

## Source contract

A Source owns its product's grants and source records. `Load` must authorize the
active actor and read one consistent snapshot in the supplied `db.Tx[db.Tenant]`
on **every** request. Use one SQL statement or hold the source/access locks until
the request transaction ends; never authorize and read in different transactions.
Return detached Input and zero Input on refusal. `ErrUnavailable` hides missing,
deleted, private and wrong-tenant sources as 404; policy denied is 403; temporary
source/policy unavailability is 503. Unexpected errors and invalid values are
integration defects (500). An absent nonzero template digest is 410. No refusal
returns cached SVG, partial SVG, an image ETag, or source/provider details.

The module declares no events, jobs, permissions or roles. GET and HEAD require
`SignedIn`; scoped bearer keys cannot use this session door. The required Source
adds object access. Missing/ambiguous providers, zero declarations, and more than
64 distinct template digests refuse dry composition before resources open.
Same-content aliases are deduplicated; declaration labels are diagnostic only.

`contracts` owns the grammar, the composed profile and the figure profile, and this
module owns authorized delivery. A field's figure reaches the kernel as a compiled
value through `entity.ChoiceFigure`, which entity declares and never parses: the
kernel draws the containing control and styles the selected state, and this owner
compiles the drawing and re-highlights it. No second SVG grammar exists on either
side of that port.

## Templates

`Compile(name, svg)` returns an immutable declaration or a bounded sentence naming
the offending construct. The label is 1–80 UTF-8 bytes without controls. The exact
UTF-8 source bytes determine `Template.Digest`; label changes do not.

Only XML 1.0 in UTF-8 without BOM is admitted. One `svg` root declares the sole
namespace `http://www.w3.org/2000/svg`. Prefixes, rebinding, duplicate attributes,
DTDs, directives, entities, processing instructions other than the initial XML
1.0 declaration, and non-whitespace text are refused. Comments and whitespace
are discarded. Standalone may only be absent or yes; encoding absent or UTF-8.

| Element | Specific attributes |
| --- | --- |
| root svg | xmlns, required viewBox, width, height, preserveAspectRatio |
| g | none |
| path | required d |
| rect | x, y, required width and height, rx, ry |
| circle | cx, cy, required r |
| ellipse | cx, cy, required rx and ry |
| line | x1, y1, x2, y2 |
| polyline/polygon | required points |

Non-root elements also admit fill, stroke, stroke-width, stroke-linecap,
stroke-linejoin, stroke-miterlimit, fill-rule, opacity, fill-opacity,
stroke-opacity, transform, data-part, data-option, data-slot. All other elements
and attributes refuse, including scripts, style, text, external/local references,
links, images, use, animation, filters, fonts, gradients, masks and nested svg.

Paint is `none` or `#RRGGBB`. Linecap is butt/round/square; linejoin
miter/round/bevel; fill-rule nonzero/evenodd. Opacity is 0–1, stroke-width 0–8192,
and miterlimit 1–1000. Numeric geometry is finite and unitless, absolute value at
most 1,000,000, using decimal/exponent syntax. Lists separate numbers with XML
whitespace or one comma; empty entries, trailing commas and `10-5` refuse.
ViewBox has four numbers with positive width/height at most 8192. Root dimensions
are positive and at most 8192; absent dimensions use the viewBox dimensions.
Radii/sizes are nonnegative. Omitted geometry preserves SVG defaults.
PreserveAspectRatio is none, xMidYMid meet (default), or xMidYMid slice.
Points need at least two pairs for polyline, three for polygon. Paths start with
M/m and admit M/L/H/V/C/S/Q/T/A/Z, either case, complete argument groups only;
arc radii are nonnegative and flags literal 0/1. Transforms are whitespace-separated
matrix(6), translate(1/2), scale(1/2), rotate(1/3), skewX(1), skewY(1).

Paired data-part/data-option names match `[a-z][a-z0-9_-]{0,31}`. A selection
retains every matching pair and removes other option subtrees. Nested
option-bearing elements refuse. data-slot="name" replaces/adds fill;
data-slot="stroke:name" replaces/adds stroke. Slots follow first occurrence in
the whole declaration, including unselected branches. Inheritance stays SVG's.

Bounds: 262144 source bytes, 2048 elements, depth 32, 20 attributes per element,
65536 decoded bytes per attribute, 16384 numeric tokens, 32 parts, 64 options per
part and 16 slots. Bounds apply before option filtering. Empty vocabularies work.

## Values and identity

Value contains exactly Options (`Part`, `Option`), ordered Colours, and Background.
Supply exactly one declared option per part and one `#RRGGBB` per slot, plus a
required `#RRGGBB` background. No defaults, alpha, padding, names or CSS are
accepted. `Resolve` copies inputs, sorts selections by part, uppercases colours,
and serializes fixed-order JSON: options, colours, background; empty arrays are
arrays. Canonical JSON is bounded to 8192 bytes. `Resolved.Canonical`, `Parts` and
`Slots` return detached copies. Zero/mismatched declarations fail closed.

Identity is SHA256 of `platformkit.picture.v1\0`, the 32 raw template-digest bytes,
the canonical length as big-endian uint32, and canonical bytes. Slot order is
significant. Composition removes markers, inserts a background rect first, sorts
attributes by name, and writes explicit XML start/end tokens without declaration,
indentation or newline. Output is limited to 1 MiB without partial returns.

## Delivery

`entity.ComposedPicture(key)` encodes 1–512 UTF-8 bytes without controls into
`c1.` plus canonical unpadded base64url. The reference is at most 686 bytes and
grants no access. GET/HEAD `/api/v1/picture/pictures/{reference}/content` accepts
no query parameters or width variant. Access precedes cache reads, conditionals,
and ranges. `ServeContent` supplies HEAD, ranges and ETag conditionals; the ETag
is the quoted lowercase SHA256 of rendered bytes. Responses declare
`image/svg+xml; charset=utf-8`, inline, private/no-store, nosniff, same-site CORP,
and `default-src 'none'; sandbox`. Failed range/validator responses remain private.

## Cache and lifetime

The runtime cache is borrowed through CacheFrom after Connect. One pool per mount
retains at most 32 SVG strings across all tenants, with non-sliding five-minute
TTL and LRU eviction. Full tenant keys separate entries; concurrent misses
coalesce. Only detached template/value inputs enter loaders, never a transaction
or access decision. Store failures preserve the existing Group fallback;
source/database failures never become successful cached responses.

## Interface

Form: derived catalog scalar (`entity.Picture`, catalog version 3). Generated web
lists fetch its authenticated SVG; native credential forwarding and SVG decoding
remain unimplemented.

## Presentation

Derived `entity.Picture` fields use `readOnly:"true" ui:"present:image"` and
`Spec.Immutable` when create/update exist. They have no form controls, widgets,
file-use entries, or sort/filter semantics. JSON Schema describes a bounded
string; the catalog document carries this scalar beside choice metadata at catalog version 3. Compose
`admincontracts.Pictures.ComposedSource` and `screens.Pictures.ComposedSource`
from `contracts.PictureSource` alongside the unchanged stored-file Source.
A missing composed source or invalid token renders an empty picture. The list
uses existing Media, alt/decorative and row-link rules; no inline SVG is emitted.

## Verification and limits

`go test ./modules/picture/contracts ./modules/picture/internal ./modules/picture`
exercises these acceptance tests:

- `TestPureDecisions`: closed grammar, values and deterministic composition.
- `TestDeclarationsAndResolvedValuesAreDetached`: detached values and template identity.
- `TestDeliveryAuthorizesBeforeWarmBytesAndHTTPValidators`: authorization, RLS and private delivery before cache/validators.
- `TestPictureCompositionRequiresSourceAndValidFiniteDeclarations`: dry composition refuses missing/invalid declarations.

The fake delegates pure decisions and refuses source reads; it does not script
product access or claim SQL conformance.
The reference composition test optionally runs the isolated Playwright journey
with `PLATFORMKIT_PICTURE_BROWSER` naming the installed Playwright executable:
`go test ./apps/platformkit -run TestComposedPictureUsesTheGeneratedListAndSessionDoor -count=1 -v`.
Its [configuration](../../e2e/picture.config.ts) selects the fixture journey;
`make e2e` also invokes this test separately from its ordinary application run,
which excludes the fixture directory. Login precedes a JavaScript-disabled context.
The delivery fixture uses real forced RLS and one locked source decision; each
product Source must prove its own grants, tenancy and revocation consistency.
`make check` is the repository gate. Uploaded SVG remains subject to the file
module's unchanged inline refusal. Stored UUID image fields keep their meaning.

Products own artwork, palettes, source keys, snapshots and record mutations.
Retained snapshots must keep their original template declaration across boots;
an absent original artifact returns 410. No product rule or artwork ships here.
This module adds list image reading only: cover/detail layouts and native
credential forwarding/SVG decoding are outside its scope.
