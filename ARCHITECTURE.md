# Architecture

PlatformKit composes multi-tenant SaaS applications from Go modules. The public
repository owns the runtime and shared UI; a downstream application supplies
the modules and configuration its product needs. This page describes the
implemented boundaries. Contribution policy lives in
[CONTRIBUTING.md](CONTRIBUTING.md), and decisions live in [docs/adr](docs/adr/).

## Start at the composition

[apps/platformkit/modules.go](apps/platformkit/modules.go) is an ordered list
of module constructors. Each constructor accepts a typed `Deps` struct and
returns a manifest. There is no runtime discovery step. The compiler checks
dependency types; composition tests check required values and selected modules.

Tenant creation uses [auth.SeedRoles](modules/auth/module.go) inside its existing
transaction. Provisioning is independent of the authentication service, so
tenants, host lookup and active-tenant enumeration exist before notification
and auth construction. Applications validate custom initial grants through
[auth contracts](modules/auth/contracts/roles.go) against their composed
permissions; auth owns the role writes and preserves existing grants on retry.

Public account creation is opt-in through `auth.Deps.Registration`. It requires
tenant host lookup and email delivery. The public endpoint queues a request;
auth's worker creates an invited member and the existing password-link flow
verifies mailbox access. Callers cannot choose roles. Retries preserve existing
accounts, including inactive accounts, and the shared mail-request limit bounds
signup and password recovery together. Without an opt-in registration capability,
no registration endpoint is mounted.

The user service also owns [pending registrations](modules/user/contracts/registration.go)
for applications requiring operator approval. `RegisterPending` hashes a supplied
password and stores application-chosen roles; it emits no invitation or password
link. `PendingRegistrations` lists the tenant's pending accounts. The explicit
`POST /api/v1/user/users/{id}/approve-registration` command requires
`user:approve`, independently of password and role management,
preserves credentials and roles, and records the approving principal. Password
recovery, password changes and sign-in cannot activate pending accounts; status
decisions share a row lock with deactivation. This service capability does not
establish mailbox verification.

Compositions requiring review supply `auth.Deps.ApprovalRegistration` with the
user registrar and trusted initial roles, instead of `Registration`. The same
`POST /api/v1/auth/register` path then requires a full name, password, matching
confirmation and accepted terms. It writes the pending account directly and
returns the same acknowledgment for an existing email. Every attempt hashes
the supplied password before one insertion attempt; no account lookup decides
the public response. An email conflict leaves the transaction usable and cannot
overwrite credentials or roles. Credentials never enter the event outbox.
This mode shares the recovery request limit and needs no email delivery. A
client still composes its terms guidance, pending-success page and review UI.

A module has three parts. `contracts/` defines its entities, public service,
events, permissions and conformance suite. `internal/` contains its
implementation. `module.go` declares the constructor and manifest.
[modules/task](modules/task/) is the reference example.

A consumer imports another module's `contracts/`, not its `internal/` or
constructor. Application composition is the place that connects them.
[scripts/check_imports.sh](scripts/check_imports.sh) checks this boundary.
Shared code belongs in `kit/` only when it is runtime infrastructure rather
than a business rule.

The import and tenancy source checks are shared tooling owned here. A
consumer runs the scripts from its resolved foundation dependency, supplies
its repository directory, and names its composed module dependencies for the
import check. It does not copy the checking algorithms. `make check` exercises
their cross-repository refusals using temporary source fixtures.

## Follow a request

The router in [kit/httpx](kit/httpx/) resolves the request context and declared
authorization before a handler reaches a service. Every operation must declare
public, signed-in or permission-based access; boot validation rejects missing
or unknown requirements. Operator permissions are separate from tenant
permissions.

[kit/db](kit/db/) owns transaction entry and tenant database settings.
`db.Tx[db.Tenant]` and `db.Tx[db.System]` distinguish tenant and system work
in Go. PostgreSQL row-level security enforces isolation for tenant tables under
the application role. The owner role performs migrations, not ordinary requests.
The tenant settings are PostgreSQL `USERSET` values, so the source gate that
restricts writes to `kit/db` is part of the security boundary, not a substitute
for database privileges. [ADR 0003](docs/adr/0003-tenancy-by-postgres.md)
describes the constraint.

A service records events in its transaction. [kit/events](kit/events/) delivers
the committed outbox through the selected transport and claims each event for
a subscription in the handler's transaction.
[Spec](kit/rest/rest.go) shares create, update and delete mutations between JSON
routes and registered resources. Updates and deletes lock the live row before merging,
validation, hooks and event snapshots. This serializes server-side decisions;
it does not reject an old form based on the revision its user originally saw.
Use [rest.Operation](kit/rest/operation.go) for typed service projections with
custom paths or response bodies; it shares transaction and error handling with
commands while the service keeps its domain authorization. Resource counts use
one read guard; standard Specs issue a COUNT without loading entity rows.
[kit/app](kit/app/app.go) defaults to an in-memory transport for the combined
`all` role and JetStream for separate `web` and `worker` roles, unless the
application supplies a transport. Memory waits for committed handling or a
terminal record before acknowledging publication; unfinished rows recover from
PostgreSQL after a restart. This does not establish the broker deployment's
durability. Terminal recording remains retryable after the handler attempt cap;
[ADR 0004](docs/adr/0004-events-are-the-job-queue.md) defines recovery and retention.

JetStream delivery is at least once. Database claims prevent repeated committed
handling; external effects still need the provider's own idempotency contract.
[kit/jobs](kit/jobs/) schedules work through that event path.

## Evolve the schema by owner

The foundation and each selected module supply ordered `db.MigrationSource`
values. Each source has a stable owner and its SQL filesystem. `kit/app`
collects the sources in composition order; there is no global version range
or flattened migration filesystem.

The runner validates the selected source files, obtains a database advisory
lock, checks applied histories, and executes each pending file with its history
row in one transaction.
Applied files are immutable. A failed file rolls back; completed earlier files
remain applied. Disabling a module retains its data and migration history.

[ADR 0011](docs/adr/0011-migration-ownership.md) defines accepted SQL, integrity
checks, the clean baseline and upgrade behavior. Append a revision to repair a
schema; do not edit the history table to make a failed migration appear applied.
An older image is not a schema rollback. A rolling release requires evidence
that both running versions can use the schema.

## Compose the interface

[design](design/) owns theme values and typography. A `design.Pair` supplies
light and dark themes. Each theme's optional `Typography` selects display, body
and mono fallback stacks; empty fields retain the defaults. Supply the same value
on both themes for shared type, and deliver licensed font assets separately.
Components name semantic roles, roles resolve to tokens, and themes supply values.

[Theme.FontFamilies](design/typography.go) projects those existing token identities
as ordered literal or generic family names. It follows the admitted
[CSS Fonts syntax](https://www.w3.org/TR/2026/WD-css-fonts-4-20260907/#font-family-name-syntax):
quoted commas remain inside one name, quoted `"serif"` is not a generic fallback,
and unquoted identifier whitespace becomes a single space. Escapes, comments,
functions and system/context keywords are refused without partial output.
The comparable theme and its trusted CSS rendering remain unchanged.
`FontFamilyToken.Validate` checks detached identities and ordered family values;
it does not reinterpret literal names as CSS or certify available font files.
`ValidateFontWeight` supplies the shared numeric domain for face metadata and
style measurements, including exact decimal checks at the 1 and 1000 boundaries.
Nonnegative scale and blur checks retain the decimal sign even on float underflow;
authored negative zero remains zero, and signed offsets/tracking remain valid.

[Asset and FontFace](design/assets.go) describe caller-owned identities, formats,
digests and provenance; each asset carries its own license evidence.
`ValidateAssets` checks the selected metadata and references without requiring
components or layout. `Asset.VerifyBytes` checks supplied asset and notice digests
without I/O. Neither check establishes usable fonts or redistribution rights.
The existing native font validator still owns format, face and glyph checks;
Core can describe weights or formats that a provider refuses. Snapshots carry
this metadata; DTCG retains it as extension evidence, not embedded files.
The [editor bundler](tools/designexport/openpencil/README.md#deliver-source-backed-font-assets)
can bind selected records to explicit local files, verify each asset and notice,
then reuse the provider's existing loader. It neither chooses product typography
nor establishes application delivery or redistribution rights.

[style.RoleColors](ui/style/emission_roles.go) projects those same role definitions
as explicit literals, references and two-input sRGB mixes. `RoleVars` renders
them to the existing CSS; there is no second role table. Compose one selected
theme's `Tokens()` with these declarations through
[design.ResolveColors](design/colors.go) to obtain fresh RGBA values. This pure
check requires complete references and rejects duplicates, wrong-type targets,
cycles and unsupported literals without partial output. Equal colours retain
different identities. Mixes use [premultiplied alpha](https://www.w3.org/TR/2026/WD-css-color-5-20260908/#color-mix),
including transparent inputs; the admitted operation is narrower than general CSS.
Only hex RGB/RGBA and `transparent` literals are currently resolvable. Other
trusted theme CSS still renders through the existing path but is not certified
by this source contract. Names use ASCII custom-property identities. The source
projection can be included in v2 snapshots. The
[OpenPencil token path](tools/designexport/openpencil/README.md#generate-a-design-document)
admits a bounded selection of colours, aliases, mixes and numeric scales; it reuses
linked icons and retains source evidence separately from native values. The source
contract alone establishes neither that native support nor layout or asset availability.

[ui.ExportTokens](ui/export_tokens.go) composes the existing colour, fallback
family, scale, shadow and timing owners without components or I/O. Select
`light`, `dark` or both explicitly; output follows selector order. Theme token
kinds other than colour and font family, including semantic shape dimensions,
are outside this typed projection. Its `TokenExport.Validate` permits detached,
dependency-closed subsets and caller-supplied asset metadata. Selected modes
must expose the same colour/font identities. Shared colour references resolve
in each mode, and transition timing must be present in the selected values,
not merely known to a source owner. Asset-only or scale-only selections need
no mode, layout or assertion that fallback fonts are installed.

[DesignExport.WithTokens](ui/export_source.go) attaches that selection to an
existing capture, producing a detached v2 snapshot with `source-tokens.v1` and
a new content hash. Selected theme values and overlapping measurements must agree
with the original capture; the operation neither edits source nor certifies
equality to rendered CSS.
`CheckSourceContract` checks the whole required-feature envelope before
dispatching to the token and layout owners. Token-only captures need no layout;
unknown layout remains unknown when it is requested. Earlier layout-only v2
snapshots retain their admission rules, and the narrower `CheckLayoutContract`
still refuses token features. Ordinary `Export` output remains unchanged.
Consumers must explicitly support the token feature before using these values;
native component and layout migration remain separate.

[TokenExport.DTCG](ui/export_dtcg.go) projects one explicit mode into the
[DTCG 2025.10 format](https://www.designtokens.org/tr/2025.10/format/) and
[sRGB colour representation](https://www.designtokens.org/tr/2025.10/color/#srgb).
An empty mode is valid only for mode-independent selections. The whole source
selection is validated first. Aliases remain references; compatible dimensions,
weights, durations, curves and layered shadows keep their typed values.
Contextual units, keywords, reference-like literal family names and source
transition declarations return no document, with every projection refusal
reported. No unsupported token is silently filtered out.

Successful output can still have diagnostics: mixes become resolved colours,
font names lose their literal/generic discriminator, linear keywords become
equivalent curves and omitted shadow lengths become explicit zero px. The
`dev.septagon.platformkit` extension profile `dtcg-2025.10.v1` records the mode
and diagnostics at the root, and `sourcePath` plus the original `source` value
on each token. Asset/face evidence remains root metadata, not font bytes or a
standard asset token. Names escape `%`, `.`, `{`, `}` and `$` as percent-encoded
bytes, so `0.5` becomes `0%2E5` without colliding with an already escaped name.
This is output-only interchange, not a resolver, registry or universal round
trip. DTCG is a Community Group report, not a W3C Recommendation; source Go
remains authoritative and consumers must review diagnostics before using files.

[ui/icon](ui/icon/) owns icons. [ui/components](ui/components/) provides typed
Go functions returning HTML and declares the classes those functions can emit.
[ui/style](ui/style/) resolves the declarations to CSS.
[ui.Compose](ui/ui.go) combines the shared declarations and a consumer's own
classes and rules into a stylesheet value. Deleting a component should not
leave an independently maintained stylesheet behind.

[css.Sheet](ui/css/css.go) retains ordinary rule contribution order; only
adjacent equal selectors share a block. Repeated declarations remain ordered
so the browser can apply priorities, fallbacks and shorthand semantics. A later
consumer override must not be merged into an earlier rule ahead of utilities,
nor carry unrelated earlier declarations forward. Shared utility deduplication
belongs to the style owner, before emission. The existing at-rule group still
renders after ordinary rules; this is not an arbitrary CSS parser or minifier.

[Gallery](ui/components/gallery.go) captures the existing constructor calls with
stable identities, typed properties and named Go slots. Passing a captured
example's `.Node` into another constructor retains its source identity without
another rendering API. The gallery still renders through those constructors. Property edits and
supported `Node` or `[]Node` slot replacements produce another bound example;
slot nodes are trusted Go capabilities, not user-supplied markup. Callbacks
and compound slot data are described but are not portable replacement inputs.
Nested identities are local to their enclosing occurrence, not array positions
or labels. The gallery's explicitly composed icons and actions retain these
identities too; changing an icon's glyph is a nested property edit, retaining
its size and tone, not proof of arbitrary component substitution.
Export retains declared slot ownership and byte spans from one
synchronous rendering. A missing span means unobserved, not absent: opaque nodes
can buffer or duplicate output. `OpaqueSlots` identifies unbound slot inputs;
these records do not certify complete composition through arbitrary Go wrappers.
Examples sharing a component identity must agree on property editability,
the property schema and every named slot declaration, including nested and
unobserved children; slot declaration order and
example content do not change that interface. Navigation tabs, panel tabs and modal
helpers retain distinct contract identities even when grouped together in the
gallery. Alert and EmptyState expose their existing typed slot constructors to
consumers, so their gallery examples do not require private rendering adapters.
Replacing a public captured node or changing its identity invalidates typed
projection and edits; a proposal must resolve the original source capture.
Omittable concrete string fields advertise `default: ""` when omission means
their definite Go zero value. Required strings, pointers and fields promoted
through optional pointers do not gain that default. Serialized Props retain
their existing omission behavior; these defaults do not describe renderer fallbacks.

[ui.Export](ui/export.go) projects those examples with their palette, glyphs
and stylesheet into a content-addressed snapshot. Products supply their own
bound examples and reuse that boundary. An OpenPencil adapter must translate
the snapshot and separately prove native editing, sizing and save/reopen
behavior; the snapshot itself is neither another registry nor a JSON runtime
engine for constructing pages. Products own their source identities; Core's
`pk-ui.component.` prefix is a namespace, not a repository reference.

`ExportWithLayout` opts into `platformkit.design-export.v2`; `Export` and
`Example.Describe` retain the v1 observation contract and its bytes.
`Example.DescribeWithLayout` records source-owned root declarations from the
same resolved values that Stack and Flex render. The required feature
`source-flex-declarations.v1` covers direction, governed gap step, alignment,
justification and wrapping, not sizing, typography, child placement or editing.
Gap names a [style spacing step](ui/style/spacing.go), not a pixel measurement
or a new design token. `normal` explicitly means CSS initial alignment;
missing layout means unknown, never an inferred default.

The [layout contract check](ui/export_layout.go) is a pure, all-occurrence
preflight on source-generated Go values, not an untrusted JSON decoder or hash
authenticator. Unknown versions/features and malformed declarations are unsupported;
unmigrated, unobserved or unowned layout is unknown. These are distinct errors.
The initial admitted examples are directly captured Stack/Flex trees with
bound children and no escape hatches. Unknown child layout prevents acceptance
of the whole tree. A successful preflight establishes only the declared feature,
not native support, visual equivalence, accessibility or source-write authority.
Providers still owe independent rendering and requested-edit checks, including
the containing source path. They must refuse before mutation if those fail.

Consumer classes, attributes, hidden roots and opaque slots cannot earn a known
layout declaration. Nonempty extra sheets invalidate declarations throughout
the snapshot, including inactive media rules: no second CSS interpreter tries
to prove which selectors might apply. This deliberately conservative boundary
can later narrow through an owned resolution path, not one screenshot.
Occurrence layout does not change `SameInterface`; replacement compatibility
and projection support remain separate questions. The v2 hash covers layout
and required features as well as the existing source content, without timestamps
or provider IDs. The existing native adapter remains v1-only and rejects v2;
complete component migration and native translation are subsequent work.

The v2 export also requires `source-measurements.v1`. Its detached measurements
project the existing [style scale owner](ui/style/measurements.go): numeric
spacing steps, font sizes, their paired line heights and font weights. Scale
and key form the reference; Flex's gap names the `spacing` key. Numbers retain
their decimal source encoding. Units are `px`, `rem`, or explicitly empty for
a unitless number; a line-height multiplier is not a pixel length. The layout
preflight requires unique, valid measurements and every referenced gap when
that feature is present; a dependency-closed subset need not carry unused scales.
Earlier v2 declaration-only snapshots remain recognizable without inventing
measurements, and consumers that do not understand a required feature refuse it.
These values come from the same tables/functions as CSS, not another registry.
They do not configure a second palette or certify consumer overrides. Layout
keywords such as `auto` and `full`, remaining scales, DTCG interchange and licensed
font delivery are outside this first measurement scope. The separate source
colour declarations above do not change this snapshot contract.

[ScaleValues](ui/style/scale_values.go) extends the source projection with
spacing keywords, tracking, leading, radii, maximum widths, breakpoints and
duration steps. Numbers retain their decimal source and explicit units; `auto`
and `none` are keywords, not zero lengths. Font-relative, root-relative,
viewport and percentage values require their actual layout context before
conversion. Every supported breakpoint remains declared, including inactive
ones. Numeric prefixes such as `2xl` use escaped CSS identifiers so the browser
can apply those rules. The earlier `Measurements` API and its v1 validation
domain are unchanged.
[ShadowValues](ui/style/shadow_values.go) preserves ordered layers, inset/outset,
signed offsets/spread, nonnegative blur, optional lengths and precise RGBA alpha.
[EasingValues and TransitionValues](ui/style/timing_values.go) preserve cubic
control points, property order and references to the duration/easing scales.
`transition-none` has no timing declarations. These fresh values share the
existing CSS owners; they are not another configuration layer. Their validation
checks source shape, not equality to default values or permission to edit source.
The browser checks exercise contextual units, responsive boundaries, shadow
layers and transition declarations. Token snapshots retain these declarations;
DTCG projects only its admitted subset and reports losses or refusals explicitly.
Native support and animation/keyframe projection remain unfinished work.

[ui.ProjectProps](ui/proposal.go) and [ui.ProjectReplacement](ui/replacement.go)
accept a base export hash and exact occurrence ID segments. Typed patches change
properties; replacement copies a compatible invocation's inputs and renderer,
retaining destination metadata. `SameInterface` compares component identity,
schema and slots. Traversal rebuilds owning slots from immutable base inputs;
self-replacement is permitted. Projection requires observed, directly owned
occurrences and revalidates after rendering. Opaque ancestors and retained-old
capture aliases are refused. These operations run trusted constructors in memory;
they provide no authentication, effect rollback or persistence.

[ui/source](ui/source/source.go) adds development-time persistence for existing
keyed string literals. The caller supplies its Go producer and an explicit call
position with a file hash. Typed AST edits must rebuild to the full proposed
export; source/build revisions are rechecked before one atomic file replacement.
No ID registry or provider type enters this boundary. Unix advisory locks serialize
cooperating writers, but cannot exclude an unrelated editor's final rename race.
The [tooling guide](tools/designexport/README.md#persist-a-string-property) owns
prerequisites, review/apply commands and limits; child insertion remains separate.

[RequestNoticeExamples](ui/page/page.go) captures the recovery content that
`page.Document` already serves, retaining its Stack, Alert and Link contracts.
Consumers may include those notices in source compositions; the document still
owns their hidden wrappers and the request controller owns when they appear.
Capturing a notice does not execute recovery or establish a connected prototype.

[tools/designexport/openpencil](tools/designexport/openpencil/) owns native
adapter tooling, not another component catalog. Its version- and source-checked
SDK corrections operate on build/process inputs without modifying an installed
editor. Native conformance runs separately from Go checks; a correction also
needs verification in the browser build before editor release.
The generator reads the current Go export and produces native token variables
and linked icon components. Explicit example selections add supported linked
components and editable placements; one unsupported selection rejects the document.
Its guide owns conversion limits and font prerequisites; product flows remain unfinished.
Browser observations reuse the exported HTML and CSS rather than reimplementing
Go components. Source-owned text comments identify exact property regions without
adding layout elements. Observations and supplied-font checks are converter inputs,
not proof of native component editing or slot replacement. Experimental native
construction binds observed text, literal text controls and explicitly supplied
single-SVG slots. Nested composition retains linked masters for observed occurrences.
Declared slots retain independent source ownership; constructor-internal captures
retain source-derived inputs without inventing replaceable slots.
The [adapter guide](tools/designexport/openpencil/README.md) defines supported proposals and fidelity limits.
Native tooling and tests have their own reviewed source budgets, separate from
the application's browser controllers.

[ui/page](ui/page/) models a document as shared `Chrome`, a request value,
a handler's `View` and a composing `Frame`. `page.Serve` adapts that
composition to the router. [ui/screens](ui/screens/) renders resource screens
and describes the resource catalog at `/api/v1/admin/resources`.
The admin module and downstream storefronts call these packages rather than
maintaining separate document or stylesheet machinery.

The shared [Video](ui/components/video.go) component uses native playback and
caption controls without autoplay. A composing page supplies a nearby transcript
and authorized media URLs. File consumers reuse
[ContentResponse](modules/file/contracts/response.go) after checking access on
every request; seekable storage supports byte ranges and HEAD without a second
streaming implementation. Playback position and course completion belong to the
consuming learning capability, not the shared player.

Resource schemas drive record-management screens, field choices and value
display. Product-specific interactions use explicitly composed pages and their
own journey tests. Schemas are compiled Go values, not a runtime page builder.
A native shell consumes the resource catalog over HTTP; its UI is a separate
consumer of that contract.

The browser uses vendored htmx and the controllers under
[ui/assets/js](ui/assets/js/). There is no framework or CSS compilation step.
The theme follows the operating system until the user explicitly chooses one.
That choice is stored locally and restored before first paint.

## Keep delivery boundaries explicit

A downstream application pins the public module and adds its own composition.
Private catalog capabilities depend on the public contracts; the public module
never imports a private repository. Client configuration selects capabilities,
assets and branding. Client-specific Go belongs in a module, not in a
configuration directory.

The reference binary supports `--role web|worker|all`. Those roles do not make
every deployment topology safe: shared storage, schema compatibility, migration
ownership and provider behavior need environment-specific verification.

[kit/httpx](kit/httpx/) sets response security headers and a request-nonce
content security policy. Inline style attributes remain an explicit allowance.
The file module rejects unsafe inline content and validates declared renderable
types against uploaded bytes. Read the corresponding code and tests when
changing these boundaries; a proxy or browser assumption is not evidence.

## Verify the boundary you changed

[Makefile](Makefile) defines the local checks.
`make check` runs build, vet, formatting, real-service tests, source and
package budgets, import checks and tenant-setting checks. Boot and authorization
cases are part of those tests. `make e2e` separately exercises the admin shell
and a generated CRUD journey in a browser.

[loc-budget.json](loc-budget.json) and
[packages-budget.json](packages-budget.json) hold current ceilings. Do not copy
their numbers into prose; run `make check-loc` and `make check-packages`.
[The CI workflow](.github/workflows/ci.yml) also runs dependency vulnerability
analysis. [The release workflow](.github/workflows/release.yml) checks the tagged
tree before publishing its image and SBOM.

A type check proves types, a test proves its exercised cases, and a product
journey proves its observed outcome. None alone proves production readiness,
design-file fidelity or reuse across different products. Report those claims
only with evidence at the same scope.
