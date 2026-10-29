# Saved — specification

Status: the module exists. [Contracts](contracts/saved.go), the
[shared decisions](internal/decision.go), the
[conformance suite](contracts/savedtest/conformance.go) both adapters answer, the
SQL service over `saved_items` and the [pkit declaration](module.go) — its needs,
provides, contributions, grants, events and personal-data claim — are written, and
the suite runs against Postgres. What is not written yet is the composition half:
no route, no picture delivery, no heart and no grid. Those sections below are
**intended behavior**, not a claim that those surfaces exist.

Saved is an optional foundation capability. A person retains a source-authorized
copy of anything identified by `(module, kind, key)`. Its first title, resolved
values, picture, source version and capture time survive source changes and
source removal. Retaining a copy grants no right to execute a live source action.

## Reused, added, made reusable

**Reused**: this delivery composes `kit/db`'s tenant transaction, `kit/crud`'s
locked read, `kit/events`' outbox, `kit/dbtest`'s per-case schema, `kit/porttest`'s
conformance harness and `modules/picture`'s compositor, and follows
`modules/task`'s shape for a module-owned migration and a service over it.
**Added**: one person-private retained-copy lifecycle — a source-authorized
snapshot that outlives its source and a person's own private list of them —
because no kernel module and nothing on the shelf holds a copy that survives the
thing it was taken from, and no catalogue module keeps one per person rather than
per household.
**Made reusable**: `savedtest.Suite` is one executable contract that any adapter
answers, so a product's own saved store is written by passing the same cases, and
`internal/decision.go` holds every rule once for the fake, the SQL service and
whatever comes after them.

Each deliverable has one owner and the following reuse basis. “New” identifies
the bounded missing behavior, not permission to replace the foundation underneath.

| Deliverable | Reuse decision and shelf layer |
| --- | --- |
| Personal saved lifecycle and contracts | **New, because** the trace's catalogue inventory found household-owned `personal` plans and instrument-only `watchlist`, neither a person-private generic retained copy; old-platform design favorites are a flag on a design, and collection favorites retain an album reference with no snapshot. No catalogue or former-client implementation is imported or copied. |
| Module declaration, transaction, outbox and migration ownership | **Composed from** foundation `pkit`, [task's provider](../task/provider.go), [db.Tx](../../kit/db/tx.go), [events.Publish](../../kit/events/events.go) and [task migrations](../task/migrations.go), all named by the trace. The only new table is saved-owned. |
| Savable declaration and server policy | **Composed from** foundation [EntryHints](../../kit/entity/hints.go), [rest.Spec](../../kit/rest/rest.go), [Operation](../../kit/rest/operation.go), [ReadSource](../../kit/rest/source.go) and [present.Resolve](../../kit/entity/present/resolve.go); new typed source callbacks supply the missing capture/authorization decision. |
| Detail heart | **Composed from** foundation [screens](../../ui/screens/screens.go), [resource](../../ui/resource/resource.go), [ButtonWithSlots](../../ui/components/button.go), [Icon](../../ui/components/icon.go) and [entry words](../../ui/resource/hints.go). The old collection heart is behavioral comparison evidence, not importable UI or code to copy. Add a heart glyph to the existing icon owner. |
| Saved grid | **Composed from** foundation [page.Serve](../../ui/page/serve.go), [Grid](../../ui/components/layouts.go), [CardWithSlots](../../ui/components/card.go), [Media](../../ui/components/media.go) and [EmptyStateWithSlots](../../ui/components/empty_state.go). Catalogue `personal` demonstrates that composition; its internal page is not a dependency. New saved-owned markup lives only under `internal/ui`. |
| Retained picture | **Composed from** foundation [picture contracts](../picture/contracts/template.go), [compositor](../picture/contracts/value.go), [file-use ledger and Opener](../file/contracts/file.go). New owner-checked delivery exposes a saved image; no image codec, source-URL proxy or upload exception is added. |
| Conformance fake and cases | **Composed from** foundation [porttest](../../kit/porttest/README.md) and task's harness pattern. Saved-specific decisions in [internal](internal/decision.go) are shared by the fake and future SQL service; SQL cases reuse [dbtest](../../kit/db/dbtest/dbtest.go). |
| Browser evidence | **Composed from** foundation [kernel steps](../../e2e/steps/kernel.ts), [content steps](../../e2e/steps/content.ts) and the [composed-picture fixture](../../apps/platformkit/composed_picture_test.go). Only the saved-specific actions/assertions are new. |
| Personal-data participation | **Composed from** foundation [personaldata.Part](../../kit/personaldata/personaldata.go) and task's contribution. No export orchestrator or retention scheduler is added. |
| Authority adapter | **Composed from** the trace's Work Access pattern and existing user/auth contracts; the narrow locked-reader addition stays with those authority owners. Saved owns no membership or grant store. |
| Command replay guard | **Composed from** the trace's [httpx.Auth](../../kit/httpx/auth.go) and existing operation recovery; extend that hook with retained-answer visibility, without another receipt store. |

The trace resolves this foundation at `988e95c1ebf6d2ddc3a78cd514cc40a43f5bab90`
with no catalogue dependency; a catalogue or clients mirror observation proves
nothing about what consumes this version, and the task's state report records the
pins and the old-shelf search revisions and results.

## 1. Entity fields

`Item` is a command-owned read model, not generic writable CRUD. Persist the
following columns in `saved_items`; contract JSON and SQL storage structs need
not have identical nesting. IDs are nonzero UUIDs; source strings are case-sensitive.

| Column | Required meaning and validation |
| --- | --- |
| `id` | Server-generated UUID, stable for this copy's lifetime. A new save after removal gets a fresh ID. |
| `tenant_id` | From `db.TenantOf(tx)` only, never an input. |
| `person_id` | Current authenticated member returned by `Access.Current`, never an input or a chosen subject. |
| `source_module`, `source_kind` | Each matches `[a-z][a-z0-9_]{0,39}`; identifies exactly one contributed Savable. |
| `source_key` | 1–512 UTF-8 bytes, not whitespace-only, no control characters. No trimming, case folding, delimiter splitting or recanonicalization. |
| `revision` | Always 1: the saved copy is immutable. Unsave must quote it for a present own row. |
| `title` | Source-produced, nonblank, at most 512 UTF-8 bytes, no controls; always displayed as text. |
| `locale` | Source-produced language tag, nonblank, at most 64 bytes, no controls. Source owns tag validity; the generic layer does not translate retained content. |
| `source_version` | Opaque nonblank source revision, at most 128 bytes, no controls. Compared exactly with the caller's observed version during capture. |
| `values` | Valid UTF-8 JSON object, at most 65,536 input bytes including surrounding whitespace, retained as text bytes without JSON-number round-tripping. It contains all resolved facts needed to reproduce the choice. No executable HTML, callbacks or current-source lookups are interpreted from it. |
| `picture_file_id` | Nullable immutable file UUID, held using FileUses; exclusive with composed-picture columns. |
| `picture_svg`, `picture_template_digest`, `picture_value` | Nullable exact output of the existing constrained compositor, original template digest and its canonical resolved input. The compositor's existing output bound is 1 MiB. All three absent, or all three present. |
| `captured_at`, `created_at`, `updated_at` | Equal on creation, stamped by the clock the adapter is given — the SQL adapter stamps `db.Now` — UTC, microsecond precision. Never moved by a duplicate save or an availability read. |

No persisted availability, last-viewed time, mutable title, collection, order,
tag, soft-delete flag, receipt table or tombstone. The inherited entity base is
an adapter convention; `deleted_at` is not used to retain removed saved copies.
Snapshot values are consumed by Get/export and source-owned retained readers;
the generic grid consumes title and picture, without guessing a sector schema.

## 2. Identity and relationships

Unique `(tenant_id, person_id, source_module, source_kind, source_key)` over live
rows. Index `(tenant_id, person_id, captured_at DESC, id DESC)` and
`(tenant_id, person_id, source_kind, captured_at DESC, id DESC)`. A module filter
is additionally supported to disambiguate equal kind names across sources.

There is no source-table foreign key: deletion must not cascade into snapshots.
There is no upward import of a source implementation. Sources own canonical keys:
different resolved variants must have different keys; version changes alone do
not create another selection. A provider may not recycle a key for a different
thing while retained copies can name it. That is an immutable identity violation.
References from a saved copy to files use the existing ledger rather than another
join table. Tenant/member erasure follows the personal-data contract below.

## 3. States and transitions

Persisted state is absent or saved. `Save` from absent captures and inserts one
item and `saved.saved`; `Save` while saved reauthorizes and validates current
capture but returns the original item without a write/event. A source moving to
v2 does not refresh a v1 copy. A caller quoting stale source v1 is refused even
on a duplicate; reading the live source v2 permits a new attempt that still
returns the retained v1 copy. `Unsave` hard-deletes a present own item and emits
`saved.unsaved`; absence succeeds silently. Removing the last item is valid.

Availability is a **read observation**, not another saved state:

| Source.Check result | Retained read | Live open |
| --- | --- | --- |
| Success | `available` | Must still call Source.Open afresh. |
| Missing/deleted/provider no longer composed | `unavailable`, “No longer available” | `ErrUnavailable` (**immutable** for the named source). |
| Access revoked | `unavailable`, same words | `ErrDenied` (**correctable** through the source's authority owner); no destination disclosed. |
| Provider timeout/outage | `temporary`, “Temporarily unavailable” | `ErrTemporary` (**correctable** after recovery). |

Availability never causes deletion, refresh, an event or a grant. Unexpected
errors/cancellation are **correctable** and abort with zero output; a partial page is
never returned.

## 4. Authorization

`saved:read` reads only the current person's copies; `saved:write` saves/removes
only theirs. Products assign these permissions through existing role declarations.
No wildcard, operator role, owner field, tenant input or export query parameter
turns the ordinary API into “read somebody else's saved items”. Lack of sign-in
or of a current grant is `ErrDenied` (**correctable**). Inactive membership is
denied too. A valid foreign UUID and an unknown UUID both read as `ErrNotFound`
(**immutable** for that ownership/scope), and both remove silently.

Every read resolves an explicit tenant transaction and actor. SQL applies
`person_id = actor` before count, kind/module filtering, limit and offset; RLS
supplies the independent tenant boundary. Direct service calls owe the same checks
as routes. `Get`, `Find`, `List`, image delivery and `Open` return no foreign data.
`Find(Ref)` supplies the detail's pressed state without enumerating the library.
No tenant/zero transaction returns `db.ErrNoTenant` (**correctable** through a
proper tenant request); a context naming a different tenant than its supplied
handle returns `db.ErrScopeMismatch` (**immutable** for that handle). Neither
path falls back to context-only storage or opens another transaction. The fake
deliberately substitutes context tenancy for a real handle and cannot prove this.

`Access` is a required consumer-owned port. Its composition adapter reuses the
user contract's Get and auth contract's Permissions/Grants, rereading membership
and role grants in the supplied transaction, including credential scope. It
does not trust Principal.Roles captured before a wait. The adapter must hold
authority stable through commit using the existing user/auth ownership locks.
The inspected user/auth contracts lack a locked membership reader: expose that
narrow contract in its owner as part of the adapter delivery, never query their
internal tables from saved.

`Source.Capture` authorizes retaining the entire projection, including media;
permission to view a source is not automatically permission to retain it. A source
refusal returns zero and makes no changes. Save calls it on retries too.
`Source.Open` performs its own current authorization and supplies only a local
`/app/` URL. Source commands such as start/use are their existing authoritative
commands, not a saved action dispatcher. Each rechecks grants and its expected
revision in its own transaction; neither a card nor an earlier Check authorizes it.

## 5. Transactions and concurrency

Use the request's `db.Tx[db.Tenant]` throughout. No second transaction, system
scope, raw GUC or mutation via generic CRUD. Every table enables and forces RLS
with both `USING` and `WITH CHECK` using the existing tenant helper/policy pattern.
Migration owner is `saved`; its one version is `000075_saved.up.sql`, added
transactionally, with no down file and no edit to another owner's migration.

For mutations, the service obtains current authority, then serializes the
person/source tuple with a transaction advisory lock over an encoding whose
first four fields carry no delimiter. Hash collisions may reduce throughput but
cannot weaken the unique constraint. Authority is reread after
waiting. On Save, Capture runs under the source's own consistency/authority locks
and compares ExpectedSourceVersion; after any wait, reread Access and reject an
identity change. Then load the current saved row, run the shared DecideSave,
and insert only when changed. File references are locked/held through FileUses
before publication; the unique constraint is the final backstop. One concurrent
first save wins, all authorized duplicates return that same first copy.

Unsave locks the row the ID names, then rechecks current authority, that the row
is this person's, and the quoted revision before it deletes. It never substitutes
a new row with the same tuple for a now-absent old ID. Release uses,
delete and emit in one transaction. Duplicate removals and removal of a foreign
ID write nothing and publish nothing. If Source or file callbacks fail, return zero/error and
the caller rolls back the entire transaction.

Lock order is authority → saved tuple → source-owned locks → ascending file IDs.
Source callbacks may not call saved or take its tuple lock. An adapter whose
authority changes use incompatible ordering must be reconciled and tested at
composition; do not claim serializability from a fake mutex. Database deadlock or
serialization failure is **correctable** by retrying the whole transaction with
fresh grants and source facts, never just its last INSERT.

Save holds the person/source lock across its source wait and loads the current row
after it, so an Unsave committed in between leaves a new copy, not an answer about
a gone row. Unsave that finds no own row has succeeded. Idempotency is within a
copy's lifetime, never durable intent replay across an intervening removal. After
uncertain delivery, clients read Find/Get before a fresh intent. A delayed Unsave
names the old UUID and cannot remove a replacement. No hidden tombstone remains.

List sorts by `captured_at DESC, id DESC`; ties are deterministic. Default 24,
maximum 100, offset >= 0; exact case-sensitive filters, unknown valid kinds give
an empty page. Other filter/sort/owner fields are refused, not ignored. Count and
page derive from one scoped SQL statement/snapshot. Offset paging is not a
historical snapshot across requests; concurrent saves/removals can move pages.

## 6. Events and idempotency

`saved.saved` and `saved.unsaved` use the typed `Changed{ID, Revision}` payload.
Tenant, actor and timestamp belong to the existing event envelope. No key, title,
values, picture or direct link is put on the bus. Current permission is still
required to fetch a saved ID. No event for duplicates, reads, provider loss or
refusals. Domain rows, file uses and outbox commit/rollback together.

Reuse the existing HTTP operation identity/receipt machinery, not a second saved
receipt store. JSON and form commands attach `Auth.RecheckingReplay`: reread
Access and the saved/source identity from the bound original request through
`httpx.RequestFrom`; no unbounded body read or different input on recovery. Save
recovery must also prove the retained result's ID still exists for that person,
and reauthorize the source/version before replay. The existing recovery hook
receives only context and transaction, not the stored answer. Extend that owner
additively with an answer-aware callback receiving the same bounded stored answer
the kernel will replay. The saved adapter decodes its typed result and compares
the saved ID/revision; never guess a replacement from the natural key. Keep the
old callback API working unchanged. Web command replies are bodyless 303s,
including enhanced submissions: recovery checks current authority and source,
then the destination/Find read supplies fresh pressed state. Cached HTML is
never replayed as a current heart. Unsave recovery rechecks current authority;
it returns no saved row.
Argument fingerprint conflict is **immutable** under that operation identity;
an expired/spent identity is **correctable** by observation and a deliberate new
intent, not blind resubmission. Failed attempts may have existing operation/failure
telemetry; the zero-effect guarantee concerns domain state and domain events.

## 7. Retention, export and pictures

Keep a copy until its person removes it or a personal-data erasure removes it.
No automatic TTL, legal hold or account-wide retention schedule is invented.
Source deletion, access revocation and provider removal do not prune snapshots.

File pictures are immutable file IDs returned by an authorized source. SetUses
uses owner `(saved, item, saved-ID, picture, captured-locale)` with `Public=false`.
The private saved image endpoint authorizes the saved item before calling Opener;
the ledger retains bytes, and does not itself authorize their delivery. No public
URL or presigned link is stored. Source-provided remote photos must first be
imported by their existing file/provider owner; saved does not fetch arbitrary URLs.

For composed pictures the source supplies the existing compiled Template and
Value. The shared decision calls Resolve/Compose and stores the exact inert SVG,
digest and canonical value. Thus removing/replacing the original template or
provider cannot change the saved image. The ordinary file upload's SVG refusal
remains in force; this is the existing composed-image contract, served as an
image, never inserted as HTML.

`GET /api/v1/saved/items/{id}/picture` rereads tenant, person and read grant before
bytes or a conditional response; use `private, no-store`, no shared cache, no
external redirect, the existing safe image-response headers and exact MIME.
Absent picture/foreign ID is 404 (**immutable** for that request). After Unsave,
the old picture URL is 404. File-storage outage is 503 (**correctable**), not
source unavailability or a reason to erase a copy.

Contribute `personaldata.Part` — privileged internal, never a caller's person
parameter: export this person's complete active copies — source, version, time,
locale and resolved values — not the retained SVG bytes, which nothing bundles yet.
Erasure is asked of the person a copy *names*, not only the keeper: the subject's
own copies go outright, and across the tenant's remaining copies it rewrites the two
columns `module.go` files as text an identifier may sit in — `title` as prose,
`values` as the document it is, rewritten inside the text its strings decode to, so
what it leaves still parses. The copy survives — it is not the subject's to take —
and loses the sentence about them. Uses release with the `FileUses` edge below; a
file picture retained now files no use. Audit and operation envelopes keep their own
retention; this takes nothing of theirs. An erasure emits no per-item unsave event;
the orchestrator records it there, and export writes and emits nothing.

## 8. Provider declaration and web contracts

What this tree holds and what it specifies are not the same list, so the line is
here rather than implied: the pkit declaration below is shipped in `module.go`
(`Provides` Service, `Needs` Access, `Clock` and every Savable, `Contributes`
personaldata.Part — §7's export and erasure), and every other row is owed: the
`httpx.Saving` adapter, the `Opener`/`FileUses` edge, and everything from
"### HTTP and generated-screen ownership" down. No application composes the
module yet, so nothing here is reachable by a person.

Implement one `New(Deps)` and one `provider.go` pkit declaration. The plan:

| Declaration | Value/read owner |
| --- | --- |
| Provides `savedcontracts.Service` | HTTP operations, saved page and source consumers. |
| Provides a narrow `httpx.Saving` adapter | Optional screens integration for lookup/command locations; an adapter of Service, not another lifecycle. |
| Needs `savedcontracts.Access` | Transaction-time membership/grant recheck. |
| Needs `[]savedcontracts.Savable` | `pkit.All` gathers typed contributions, zero or more. |
| Needs `filecontracts.Opener` and `rest.FileUses` | Retained file delivery and ledger; missing providers refuse composition. |
| Optional `*saved.Page` | Existing Shell/theme/localization supplied by the application, not by a module that depends on saved. Without it, no saved-owned web page. |
| Contributes `personaldata.Part` | Export/erasure through the existing subject orchestrator. |

Deps also contains `Clock func() time.Time`; provider sets it explicitly to
`db.Now`, test constructors supply a deterministic clock, and a nil Clock in New
is a **correctable** composition error. Missing Access/callback/file providers,
duplicate `(module,kind)` declarations, invalid words/field references, and a
declared read without a key/version are **correctable** boot refusals before IO.
Each nonempty declared word is at most 256 UTF-8 bytes with no controls and is
not whitespace-only. Empty words select the centralized fallbacks below.
One source module contributes each Savable through pkit; it does not depend on
saved.Service just to declare itself. Strings select persisted domain identities
inside that finite typed collection; they are not module discovery or a second
registry. Empty contributions are a valid empty saved library. Removed providers
leave copies readable and source actions unavailable. No branch on selected modules.

### Savable on an entity or computed read

Add a value-only `Savable` hint under existing `EntryHints` with `Module`, `Kind`,
`KeyField`, `VersionField` and `Words{Save,Saved,Remove,ListTitle}`. KeyField names
an exposed string or UUID; VersionField names an exposed string or integer.
UUIDs use canonical lower-case spelling; integer versions use base-10 without
padding; strings are unchanged. Source contributions hold callbacks; no callback,
principal, pressed state or storage service enters the serializable hint or pure
presentation policy. A source uses the same Savable value for its contribution
and conversion to EntryHints so words and identity have one declaration.
Localize words through the existing entry-key/catalogue path. Fallbacks: “Save”,
“Saved”, “Remove from saved”, “Saved items”. A list filtered to an exact
module/kind pair takes its declaration's ListTitle; other lists use the generic
title, even when kind names coincide across modules. Heart list links carry that
pair explicitly. Missing-source copies use generic copy rather than a stale
translation catalogue.

Extend `present.Resolve` with one optional resolved saving value and a narrow
capability input from its adapter. Both web and catalogue consume its localized
words/absence decision; no shell invents defaults. Absent saved capability means
no heart and no saved-list link, while the source read still works. Present but
outage means an explicit failed action, never silently pretending the capability
was not selected. Shared registrations remain immutable across actors/locales.
The additive `resolved.savable` key leaves PolicyVersion at 1, as the existing
policy contract requires for added keys. It is null without the capability.
Clone the hint before localization; existing keys retain their existing meaning.

For an existing entity, `rest.Spec.Present` is the declaration. For a computed
projection, add `Present *entity.EntryHints` to `rest.OperationOptions`, deriving
the output schema from O in **Operation's existing registration**, with the same
source key/version bindings. Publish it with that operation's existing metadata;
its catalogue representation carries the resolved value. No second
read registry, fabricated persistent entity or random key generated on display.
Pure operations remain unchanged when the option is nil. The computation's
existing result page consumes that registered metadata; this task does not add a
general operation-page generator or a product result layout. The resolved result
must expose the same key/version that Source.Capture can authorize/reproduce.

Kernel packages do not import saved. A narrow kernel `httpx.Saving` adapter
provides current state and mounted command locations to screens; saved implements
it and admin/screens receives it as a typed optional dependency. `ui/resource`
receives values and paths only. The adapter's lookup takes context, tenant
transaction and `(module, kind, key)` and returns `Saved`, saved `ID`/`Revision`,
`CanSave`, `CanRemove`, and mounted `SaveURL`, `UnsaveURL`, `ListURL`; no snapshot
or source callback crosses into rendering. Missing read permission suppresses
the control; missing write permission leaves its pressed state readable.
An unsaved result has no saved ID/revision; source version is bound from the
detail's declared field. Paths come from mounted routes, never a guessed module
registry. Dependency order is source → saved → screen
adapter. Supplying a Page must not introduce an admin → saved → admin cycle.
The new hint/option/adapter fields all have the readers named here in the same
implementation delivery; no dead manifest fields are accepted.

### HTTP and generated-screen ownership

Use `rest.Spec` with a scoped `ReadSource` for saved item Read only, plus
`rest.CommandResult` for collection Save and row Unsave. No generic Create,
Update or Delete route. List and Find are typed `rest.Operation` reads with the
same Service, because exposing a generated list would mount a second table page.
The saved module owns `/app/saved`, mounted by Page; no generated CRUD collection
page is advertised for the read-only item registration. Its page handlers call
the registered command execution path for JSON/form parity and existing CSRF,
operation identity and rollback behavior. Source detail pages remain generated
from their existing rest.Spec. The heart adds one reusable control to that path.

Command adapters use flat scalar fields `module`, `kind`, `key`,
`expectedSourceVersion` and convert them into contracts.Save. Unsave's body
contains only `expectedRevision`; the ID comes from the path. This keeps the
existing generated form/schema parser in charge without a nested Ref form.
Unsave's missing expectedRevision defaults to 0: it conflicts for a present
revision-1 copy but does not turn an already absent copy into a refusal.
The saved ReadSource projects the nested Item into a flat read-only resource:
title, module, kind, key, sourceVersion, capturedAt, revision and availability.
The full snapshot is an additional read-only JSON field, excluded from form
derivation. It remains part of Get, typed list/find replies and personal export.
Image URLs are computed by the HTTP adapter from its mounted picture route and
ID, never persisted inside the source snapshot.

| Endpoint | Result |
| --- | --- |
| POST `/api/v1/saved/items/save` | Flat save input above, 200 Item for first save or duplicate. |
| POST `/api/v1/saved/items/{id}/unsave` | ExpectedRevision, 204; absent own/foreign item is also 204. |
| GET `/api/v1/saved/items/{id}` | Read-only projection, including current availability and retained snapshot. |
| GET `/api/v1/saved/items` | Module/kind/limit/offset, Page with owner-scoped Total. |
| GET `/api/v1/saved/find` | Module/kind/key, View or 404; no implicit source capture. |
| GET `/api/v1/saved/items/{id}/open` | Authorized local source URL; web follows with 303, API returns the URL. Destination reauthorizes too. |
| GET `/app/saved` | Grid, exact module/kind filters, normal GET pagination. |

Every response is `private, no-store`. Invalid input/bounds/capture/URL is 422
(**correctable**), stale source or saved revision 409 (**correctable**), missing
actor 401 (**correctable**), missing saved grant 403 (**correctable**), private or
gone saved/source item 404 (**immutable** scope or source loss; source authority
denial remains **correctable** internally). Provider/IO failure is 503
(**correctable**); unexpected internal failures are **correctable**, with zero output. The public
404 does not reveal whether source access was revoked or the source was deleted.

### Detail heart

Render one native submit button inside the existing command/form path, using
ButtonWithSlots and the canonical heart icon. `aria-pressed` is server-owned,
true only when Find found the current person's item. Save and Unsave are explicit
commands, never an ambiguous toggle. The save form quotes source key/version;
the removal form quotes saved ID/revision. A source title, snapshot or person
field in a submitted body is refused as an unknown field (**correctable**).

Use a stable accessible name from Save (for example the declaration's “Save
look”) with `aria-pressed`; render the Saved words as adjacent status text while
pressed, and Remove as the removal description. This preserves a stable toggle
name and exposes both state and the next effect. Icon is decorative. Enter/Space
work natively, focus stays visible, hit area is at least 24px and targets 44px.
No CSS-only color state, hover-only action or animation required to understand it.
With JavaScript disabled, POST/303 returns to a validated local detail URL. With
enhancement, serialize requests per control, update after success, preserve
focus and announce status via a stable polite region. Failure keeps the prior
state and offers retry. No optimistic offline queue. Reduced motion remains
usable; missing write grant omits mutation controls.

### Saved grid

The saved-owned `internal/ui` page composes the existing Shell, Grid, Card,
Media and EmptyState. One heading uses the resolved ListTitle. A labelled kind
filter and pagination preserve only permitted query parameters. Cards show
retained title/picture/capture time and a separately labelled remove button.
Unavailable cards remain fully visible with “No longer available” and retain
remove; temporary errors show the different temporary label. No empty or hidden
cards are substituted for source loss. A saved image's adjacent title makes
the image decorative; a sole functional image is named by its saved title.
The Open link goes through the fresh source check. No command is executable
merely because it appeared in the captured values. No nested interactive card
or link wrapping a button. Normal reading order, one main landmark, 200% zoom
and 320px reflow are required. No results uses EmptyState with generic localized
copy; an empty filter does not report that all saved items have disappeared.

## 9. Locale, time and money

All persisted times are UTC from the explicit clock. List order uses captured
instants, never localized strings. Page formatting uses the existing locale/time
presentation; changing locale affects controls, not retained facts. Locale is
captured so a reader can label the snapshot's language. No price, exchange rate,
currency default or sector-specific unit is owned here. If a source includes
money in Values, its schema uses int64 minor units with Currency; saved preserves
the supplied JSON rather than converting through floating point. The source
owns semantic validation; saved owns the stated structural bounds.

## 10. Specialist validation and Limits

Sources/products own canonical key construction, version semantics, permission
to retain content/media, resolved specialist facts, language tags, accurate
descriptions, live action rules and source/provider licensing. The module refuses
malformed/oversized snapshots (**correctable**) but does not judge a recipe,
exercise, outfit, medical claim, price or jurisdiction. Source projections must
not put credentials or data the person may not retain into Values or titles. An
erase finds an identifier where the source's own bytes spell it: not a person
described rather than named, and not a name escaped into `\u0026` form.

Products choose roles, wording, themes, supported locales, file retention classes,
specialist source adapters and their versioned adoption. Catalogue household
libraries are not silently migrated into private lists. Existing sources must
adopt the declaration explicitly. Kernel reference proof adds Savable to the
existing Task detail, binding its ID and Revision, and uses the real task service
in the composed journey. Computed-read adoption belongs to the named catalogue
kitchen/training/outfit deliveries at updated ordinary dependency pins; local
declaration fixtures do not count as downstream adoption. Native UI is separate.
No shared folders, teams' favorites, search engine, bulk import, refresh/update,
undo history, quotas, recommendations, external save service or new provider SDK.

## Proof obligations

Run [RunService](contracts/savedtest/conformance.go) against the fake and against
the SQL adapter over the real table. It checks first-write events, duplicates,
source and saved revisions, zero-result refusals, owner-side private reads and
removals, source deletion/revocation/outage, a source the composition dropped,
an open's fresh destination, last removal, new identity, kind ordering, grant
recheck and detached values. Tests have independent expected snapshots, state
and events; the fake delegates the decision, not a second version of it.

Implementation must add these boundary cases without replacing that suite:

- Two first saves racing: one row, one event, identical returned ID/snapshot;
  opposite-order Save/Unsave, and two removals of the same saved ID.
- RLS with real non-owner database roles for two tenants; person filtering before
  count/page; service, HTTP, generated detail, list, picture and export boundaries.
- Revoked membership/grant/source version while queued on locks, both normal and
  receipt-recovery paths; changed result identity cannot be replayed after removal.
- Outbox/file-use failure and cancellation roll back saved row, ledger and all
  domain events; commit failure returns no successful saved result.
- Original composed template/provider removed after save: identical retained
  image bytes; file retention races deletion; former owner/other person receives
  no image bytes or conditional response. No external photo fetch or SVG upload.
- Same recipe/key family with two source-canonical variants saves twice; one
  variant retried saves once; source values never come from the request body.
- Savable entity and computed-read declarations reach server-resolved catalogue
  and web; absent capability and duplicate/invalid declaration fail as specified;
  per-actor/localized resolution leaves registrations unchanged.
- Browser journey in the real reference composition: heart/pressed/labels,
  keyboard and no-JS path, saved grid/filter/pagination/empty state, deleted source
  still displayed and live action refused. Use existing login/content steps.

Fake results cannot certify SQL isolation, transactional atomicity, deadlock
freedom, actual provider consistency, HTTP receipt recovery, accessibility trees,
responsive layout, native rendering or ordinary downstream-pin adoption.

## Dependencies

No dependency is added or upgraded. Public contracts import standard-library
values and existing kernel/picture contracts only. The decision/test adapters
reuse `github.com/google/uuid` v1.6.0 already in go.mod. The registry answer from
`curl -s https://proxy.golang.org/github.com/google/uuid/@latest` was:

```json
{"Version":"v1.6.0","Time":"2024-01-23T18:54:04Z","Origin":{"VCS":"git","URL":"https://github.com/google/uuid","Ref":"refs/tags/v1.6.0","Hash":"0f11ee6918f41a04c201eceeadf612a377bc7fbc"}}
```

Its pinned `LICENSE` was read from the module cache: copyright Google,
redistribution permitted in source and binary with retention of notice,
conditions and disclaimer, and no endorsement using contributor names. This
is BSD-3-Clause, compatible with the repository's Apache-2.0 license. No other
third-party library is selected by this specification.
