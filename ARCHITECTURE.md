# Architecture

PlatformKit composes multi-tenant SaaS applications from Go modules. The public
repository owns the runtime and shared UI; a downstream application supplies
the modules and configuration its product needs. This page describes the
implemented boundaries. Contribution policy lives in
[CONTRIBUTING.md](CONTRIBUTING.md), and decisions live in [docs/adr](docs/adr/).

## Three surfaces

Every address belongs to one of three surfaces, and the surface — not the module —
decides the middleware chain a request runs. `kit/httpx` classifies the path, and a
module mounts on a router (`httpx.Surfaces{Public, App, Ops}`) rather than writing a
prefix: `/api/v1/public/<module>/…` and `/<module>/…` for a tenant's anonymous
face, `/api/v1/<module>/…` and `/app/<module>/…` for the workspace, and
`/api/v1/ops/<module>/…` for the installation's control plane, which is served at
`app.Options.Installation`'s host only and answers as an unmounted address everywhere
else. The empty module is the composition's own namespace (`/api/v1/app/resources`,
`/app`). The table, each chain and the one-release aliases live in
[`kit/httpx/README.md`](kit/httpx/README.md); the decision and what it costs live in
[ADR 0017](docs/adr/0017-three-surfaces-by-path.md).

## Entity and presentation contracts

Follow the consumer as well as its schema; these paths share the existing Go owners.

| Contract | Implementation and consumer |
|---|---|
| Entity and command fields | [`entity.Fields`/`FieldsOf`](kit/entity/schema.go) → [CRUD aliases](kit/crud/schema.go) → [`rest.Spec`](kit/rest/rest.go)/[`Command`](kit/rest/command.go) → authorized [`httpx.Resource`](kit/httpx/schemas.go). |
| Web forms and pages | [`forms`](ui/forms/forms.go) composes shared [components](ui/components/); [`resource`](ui/resource/resource.go) renders screens from a schema and rows, [`screens`](ui/screens/render.go) adapts authorized resources to it, and [`page.Serve`](ui/page/serve.go) supplies the caller's shell and request context around a [`document`](ui/document/document.go). A refusal the kernel makes before a handler exists — the cross-site guard, a panic — is the same problem value, shaped by whoever asked ([ADR 0015](docs/adr/0015-a-refusal-has-one-value-and-two-shapes.md)): `httpx.Options.Fault`, and the application supplies `page.FaultHandler(shell)` so a browser gets a page and a client gets the JSON. |
| Native discovery | [`screens.Describe`](ui/screens/catalog.go) exposes `/api/v1/app/resources`, stamped with `catalogVersion`; the native consumer owns its renderer. |
| Sketch field | [`entity.Sketch`](kit/entity/sketch.go) validates ordered integer strokes through JSON and JSONB; [`forms`](ui/forms/sketch.go) edits them and [`components.Sketch`](ui/components/sketch.go) supplies the shared SVG reading. |
| Rich text field | A string with `ui:"widget:richtext"` enters [`richtext.Prepare`](kit/richtext/render.go) through `rest.Spec` inside the tenant transaction; the same package renders sanitized HTML for [`components.Prose`](ui/components/prose.go) in generated details and public content. [`richtext.Files`](kit/richtext/render.go) resolves images per tenant and audience, with [`modules/file.RichTextFiles`](modules/file/richtext.go) wired at composition. The schema publishes `contentMediaType: text/markdown`, and the no-JavaScript form uses a textarea with formatting help. |
| Component properties | [`Example.Describe`](ui/components/examples/example.go) derives Props JSON Schema, named slots and observed HTML from actual Go constructor inputs. |
| Design consumers | [`export.Export`](ui/export/export.go) and [`ProjectProps`](ui/export/proposal.go) produce snapshots and proposals; [source persistence](ui/source/source.go) has its own explicit API. |

Run `go test ./ui/screens -run 'ExampleFormExample|TestGeneratedForm'` to exercise
the [form-to-export example](ui/screens/design_test.go) and its composition checks.
This local check does not exercise a browser, native editor or business write.
Entity fields are flat scalars/lists or explicit semantic values such as sketches; nested answer maps need an adapter. Component
Props describe presentation, while entity and answer contracts own data validity.
A2UI/MCP adapters need separate protocol wiring; source export does not supply it.

## Four consumers constrain shared contracts

This baseline separates **observed source** from **required behavior** and
**unsupported or unverified claims**. The four scenarios below come from traced
downstream consumers at their resolved dependency revisions. Their exact pins,
private source citations and test provenance stay in the task evidence. Source
inspection establishes the paths described here; it does not establish that those
applications run on this foundation revision. The public tests linked here prove
foundation mechanisms, not execution of the four products. Older consumer wiring
is historical evidence; new composition uses the typed declarations below.

A resource grant decides who may reach a door. The domain's eligibility,
ownership and revision checks decide whether this actor may do this act now, so
in every scenario below neither one stands in for the other.

**Reused** — pkit's resolver ([resolve.go](pkit/resolve.go)): `cycleThrough`
walks the dependency edges and `cycleReasons` reads the `from`/`contributors`
maps; the composition and Describe fixtures; the resource, content, operation and
migration owners each row below names. **Added** — the declared contract and
supplier on each edge of a cycle refusal, because a module path alone does not
say which declaration to remove, and this baseline. **Made reusable** — no new
unit: the edge explanation is the refusal's sentence, which Validate, Plan/Build,
Explain and Describe return alike because all four resolve through one plan.

| Scenario | Observed contract, transaction owner and visible result | Constraint exposed by the consumer |
|---|---|---|
| Publishing | A curriculum service imports immutable source digests, reviews a revision, then publishes its active pointer and event in the tenant transaction. Staff actions reach that service through a declared operation; lesson pages render Markdown, while completions retain their content digest. This is distinct from editing a Content page. | Releasing an unreviewed revision is refused; editing released source in place must not rewrite existing completion evidence. The traced service checks publication state but does not internally recheck the manage grant or take an expected revision, and some refusals return a loaded revision. Authority and empty refusal results remain required, unproved behavior. |
| Reservations | Product forms and API commands resolve current room configuration and ownership, then call Booking Request/Transition in the request transaction. Confirmation locks the reservation and resource intervals, checks expected revision, time and Eligibility, and records history/event together. A losing confirmation rolls the request back. | Availability is a projection, not a capacity hold. A current read grant cannot confer membership eligibility or ownership. Exact committed retries recover a reservation; they do not perform another confirmation under new eligibility. |
| Commerce | A basket command checks its buyer and obtains prices from the product. Checkout invokes stock/purchase contributions under the same local transaction, then emits its event. Payment consumes that event; its collector calls the processor outside the database transaction and records settlement later. | A checked-out basket can still have a pending payment. A lost processor response is an unknown outcome, not evidence that no money moved. A cash-mode sale test cannot establish processor completion. |
| Project work | Work commands take explicit actor, command ID and expected revision. Project and command locks protect current access checks, item changes, exact response receipts, history and outbox writes. Product forms recover those receipts under current authority. DescriptionFormat is stored as plain/Markdown, but the traced display passes only Description to plain text rendering. | Resource permission cannot replace project membership, requester visibility or field-level edit authority. Replaying a receipt cannot bypass revoked access. The foundation's [SLA Task rule](modules/task/domain/README.md) is a different capability and cannot substitute for Work. |

### Retain the owners; extend only the demonstrated gaps

| Area | Observed limitation and counterexample | Required direction |
|---|---|---|
| Resource | [`rest.Operation[I,O]`](kit/rest/operation.go) accepts projections, but [`rest.Spec`](kit/rest/rest.go)/[`Command`](kit/rest/command.go) require [`crud.Entity`'s storage identity](kit/entity/entity.go). An existing catalogue reservation screen adds Base/TableName scaffolding to an already identified contract; duplicate identity/timestamp fields can change its JSON. An availability answer has no stored row at all. | Extend the existing resource/presentation owner with non-storage input/output metadata; retain operation validation and commands. Remove the reservation adapter only after proving IDs, timestamps, wire fields and absent generic write routes remain intact. A status PATCH must never replace Confirm's revision, command identity and eligibility checks. |
| Content | Markdown is a meaning even when its editor is a textarea. Work already stores a format, while its traced display drops it. Current resource media metadata follows the richtext widget. The saved string `*draft*` is literal in plain text and emphasis in Markdown. | Extend semantic format propagation through the existing entity/rest, content/richtext and file owners, independently of editor selection. Preserve saved meaning and publication digests. A successful generic CRUD upload does not prove custom Work commands validate private attachments or roll file-use writes back. |
| Operation | Existing operation/catalog schemas describe command fields and presentation without a complete retry/effect/completion contract. AddItem repeated twice increments twice; Checkout recovers the existing transition. Equal-amount refunds can be distinct intents. | Extend existing descriptions and [`httpx` receipts](kit/httpx/operation_receipt.go), retaining domain receipts and the catalogue's refund intent/reservation owner. Do not create another journal. Describe local commit, outbox delivery and external completion separately; a timeout must not turn an uncertain refund into available funds. |
| Payment subject | The traced payment contract's `SubjectKind`/`Subject` supports cart and subscription; the actual payers use cart. No traced reservation or Work operation needs another payable kind. | Retain that closed set. “PaymentSubject” is a description, not a new type or generic entity reference. A named consumer must justify another subject before the shared contract grows. |

A port accepting `db.Tx[db.Tenant]` can commit the writes performed through that
local transaction. It cannot commit a remote charge, policy decision or another
server's write atomically with SQL. A remote substitute needs proof at its actual
consistency boundary. Provider denial and outage both refuse mutation, but are
different outcomes; an outage cannot silently become optional-provider absence.

### Responsibilities and recovery across eight dimensions

Here, **correctable** means the caller can change input, authority, configuration
or revision through its legitimate owner. **Immutable** means retry cannot change
the recorded identity, retained history or prohibited scope of that request.
These are recovery explanations, not new error codes or a promise that every
command is safe to repeat. An unknown remote outcome needs observation and
reconciliation under its original identity before another effect is attempted.

| Dimension | Responsibility, invariant and state owner | Invalid combination and recovery | Executable boundary and remaining proof |
|---|---|---|---|
| Product variation | **Required:** products choose content, roles, policy inputs, prices, locale and branding; each capability owns its generic state/invariants. Every declaration has a reader. | Product-specific branching in a shared module is outside its scope. Missing required policy configuration is correctable at composition; supported optional absence must be explicit. | [Composition fixtures](pkit/compose_test.go) exercise declared selections. Actual four-product boots and ordinary-pin adoption remain unverified. |
| Composition | **Observed:** pkit resolves typed needs, selections and contributions before kit/app opens infrastructure. No domain transaction exists during resolution. | Missing provider, conflicting selection and dependency cycles are correctable declaration errors; reordering Use cannot remove a directed cycle. Empty optional needs and contribution lists are valid. | Existing missing/conflicting/cycle cases in [compose_test.go](pkit/compose_test.go), refusal parity in [describe_test.go](pkit/describe_test.go), and no-effect refusal in [build_test.go](pkit/build_test.go). These do not prove consumer adoption. |
| Identity / ownership | **Required:** Postgres RLS and an explicit tenant transaction combine with current actor/grant/scope/revision checks inside domain commands. Refusal writes/emits nothing and returns no stale row. Work Access, Booking Eligibility and buyer ownership remain distinct owners. | Foreign-tenant/owner access is immutable for that scope; stale revision is correctable by an authorized fresh read/new intent. Changing actor/input under a retained ID is immutable for that ID. Refuse removing a final required member, never a write merely finding none. | Public [Content conformance](modules/content/contracts/contenttest/conformance.go) is a mechanism example. Composed revocation, ownership and the publishing authority gap need separate consumer proof. |
| Workflows | **Observed source:** publication, reservation, checkout/payment and Work each own transitions, snapshots and receipts. **Required:** local state/history/outbox commit together; generic writes cannot bypass commands. | Unreviewed publication, overlap, exhausted stock and stale edits are correctable through their owners; released evidence, a started reservation slot and changed retained intent are immutable in their original scope. A SQL rollback cannot reverse a remote effect. | [Operation transaction cases](kit/rest/operation_test.go) establish the shared door. Domain transition, concurrent exclusion and composed rollback/recovery proofs remain with each service. No generic workflow engine is implied. |
| Presentation | **Observed:** entity/rest/httpx and ui/resource/screens/page own metadata, HTTP and generated screens; product pages own their presentation. Content/richtext/file own rendering and file references. They own no extra domain state. | Generic mutation of a command-only projection is unsupported. Contradictory format declarations are correctable before deployment; silent reinterpretation of stored plain text and cross-scope attachment use are immutable policy violations. | [Command](kit/rest/operations_test.go), [screen operation](kit/rest/screens_operations_test.go), [form/export](ui/screens/design_test.go) and [file storage](modules/file/contracts/filetest/storage.go) cases are foundation proofs. Actual JSON/form/native parity and custom-command attachment rollback remain owed. |
| Upgrades | **Required:** the existing migration ledger and module owners preserve applied checksums, tenant ownership, immutable source/history, formats, file references and pending identities from named supported starts. | Changed applied SQL is immutable for that version. Transient failure may be correctable through the runner's transaction/retry boundary; deleting data or falsifying a checksum is not recovery. Unsupported starting versions are not certified. | [Migration ownership](docs/adr/0011-migration-ownership.md) and the [populated rehearsal](scripts/rehearse_migrations.sh) define the foundation boundary. Four populated consumer upgrades/restarts remain unverified. |
| Operations | **Required:** runtime/service owners distinguish local commit, delivery and external completion, retaining actor/input/intent identity across uncertainty. Clock inputs, UTC timestamps and int64 minor-unit amounts with Currency remain explicit contracts. | Bound-input changes are immutable. Outage is correctable infrastructure failure but can leave an unknown remote outcome; observe/reconcile it rather than inventing success, failure or a fresh ID. Legitimate new intents get distinct IDs. | [Transport receipt cases](kit/httpx/operation_receipt_test.go) do not establish domain receipt equivalence or remote exactly-once execution. Controlled provider failures, refunds, restart and deployed recovery remain owed. |
| AI-assisted development | **Required:** inspection names exact source/dependency, declared operation and actual proof result. Existing Describe/Explain and product evidence owners record facts; generated text grants no authority. | Stale source or missing/skipped proof is correctable by re-resolution and execution. Private paths in public documentation or a fluent readiness claim without evidence are outside this artifact's scope. | [Stable description and fixture agreement](pkit/describe_test.go) prove composition projection, not runtime business behavior. Exact operation/proof linkage and a finite coverage report remain owed; no runtime AI module is introduced. |

The source observations vary by consumer as follows. **S** means observed in the
traced source, **U** means the named runtime proof is unverified. Every cell has
its own obligation; a shared fixture's success does not fill any consumer's U.

| Dimension | Publishing | Reservations | Commerce | Project work |
|---|---|---|---|---|
| Product variation | S editor roles/curriculum; U boot | S membership/room policy; U boot | S payment and cash selections; U paid boot | S project configuration/Access; U boot |
| Composition | S commerce/file needs; U current-pin adoption | S Eligibility adapter; U current-pin adoption | S cart contribution/provider; U current-pin adoption | S one Access provider; U current-pin adoption |
| Identity / ownership | S route grant/revision scope; U service-time authority | S ownership/eligibility locks; U revocation | S buyer/server price; U revocation | S actor/project/command locks; U revocation |
| Workflows | S reviewed release/digest evidence; U execution | S confirm/cancel/exclusion; U execution | S checkout versus settlement; U remote completion | S stages/receipts/history; U execution |
| Presentation | S Markdown/lesson-specific rendering; U attachment/browser proof | S forms and newer catalogue projection; U adopted wire parity | S basket API/view; U native/generated parity | S stored format/plain display gap; U format/media adoption |
| Upgrades | S revision/digest storage; U populated restart | S legacy snapshot test; U populated restart | S pending identity storage; U populated restart | S installed-ledger test; U mixed-format restart |
| Operations | S publication/download paths; U lost response | S exact retry versus eligibility; U restart | S collector/refund intent sources; U ambiguous outcome recovery | S exact receipt/commit-failure cases; U runtime recovery |
| AI-assisted development | S source/requirement inputs; U runtime proof linkage | S source/requirement inputs; U runtime proof linkage | S source/requirement inputs; U runtime proof linkage | S source/requirement inputs; U runtime proof linkage |

### Missing proof owners and limits

These work items identify intended acceptance boundaries, not delivered features
or evidence of completion. A newer catalogue capability is not adopted merely
because its source exists; products must resolve a version that contains it.

| Owner | Required proof or change |
|---|---|
| T-0561 catalogue / T-0562 products | Ordinary versioned dependencies and typed composition in the actual selected applications, without replace/go.work. |
| T-0580 upgrades | Named supported source revisions, populated fixtures, transactional retry and checksum refusal; T-0591/0592 adopt those fixtures. |
| T-0581 resource / T-0582 reservations / T-0583 native operations | Compatible non-storage metadata through existing owners; real reservation adoption preserving JSON/timestamps, eligibility, revisions and absent CRUD routes; actual native renderer/runtime command behavior. |
| T-0584 content / T-0585 Work / T-0586 native content | Semantic format independent of widget, shared attachment validation/use accounting on custom commands, bounded Work Description/DescriptionFormat adoption, and native editor/audience behavior. |
| T-0587 refunds / T-0588 provider consistency | Existing distinct refund intents through duplicate delivery, dispatch, restart and unknown outcomes; the selected policy seam's deny/outage, tenant/actor/action, cancellation and revocation behavior. This does not certify remote substitution for transaction-bearing ports. |
| T-0589 operation semantics / T-0590 evidence | Retry/effect/completion descriptions through existing operation/OpenAPI/catalog/receipt owners; exact versioned source/operation/proof links with stale or missing provenance visible. Description must execute no command. |
| T-0591 reservations and commerce | Actual composed ownership, eligibility, competing reservations, stock rollback, pending/refund recovery and populated upgrade/restart. |
| T-0592 publishing and project work | Actual publication and mixed-format Work upgrades, retained immutable evidence, attachment audience, revoked authority and restart. Explicitly assign repair of the observed publishing grant/revision/error-result gap if outside the proof task's remit; leave that proof unverified until repaired. |
| T-0593 coverage | Four consumers by eight dimensions, actual runtime selections and named missing/unsupported cells; no readiness score inferred from this baseline. |

Products retain curriculum/review staffing, room/member policy values, prices,
refund/fulfillment policy, project roles/stages/fields, audience/retention,
language/timezone/currency and production provider choices. This baseline adds
no migration, retention policy, payment subject, registry or universal model.
It does not establish all-version compatibility, browser/native parity,
translation coverage, live processor behavior, backup/restore readiness,
operational capacity or production readiness.

## Provider boundaries

Reuse the existing owned contracts: [event delivery](kit/events/README.md), File `Storage`, Notification
`Mailer` and `tenancy.Policy`. [NATS configuration](config.example.yaml) selects
broker transport, authentication and trust at composition. [Page localization](ui/page/README.md)
uses the portable [locale contracts](kit/locale/README.md); [xtext](kit/locale/providers/xtext/README.md) owns catalog formatting, with page aliases for existing callers.
[Feature evaluation](kit/flags/README.md) uses `flags.Evaluator` with isolated
OpenFeature/OFREP provider packages; constructor imports migrate as described in that guide.
[Topaz](kit/tenancy/providers/topaz/README.md) implements `tenancy.Policy` without importing Auth.
Flag evaluation does not administer flag definitions.
Provider administration belongs to each capability's authorized resources and commands,
rendered through `rest.Spec`, `httpx.Resource` and `ui/screens`. Private resources
set `OperatorRead` and `OperatorWrite`; API routes, resource closures, discovery
and generated screens enforce the same declarations. These adapters alone do not
establish management screens, deployed connections or migration of downstream clients.

## Independently usable parts

[Wire](kit/wire/README.md) checks a composition's OpenAPI or AsyncAPI contract
against its published golden using B1–B6. It links only the standard library;
the reference composition delegates both document gates to it. Products supply
their renderer, golden path and explicit reviewed authorization allowances.

Import the owner of the capability you need: [entity](kit/entity/README.md) for
field metadata without CRUD, [forms](ui/forms/README.md) for captured controls
without REST, [locale](kit/locale/README.md) for worker or page translations, and
[Task domain](modules/task/domain/README.md) for the deterministic resolution rule.
Their package guides contain executable entry points and dependency limits.
The three refusal sentinels are a fifth part: [fault](kit/fault/README.md) holds
the refusals a caller can act on and links nothing beyond the standard library,
so a value package, an `events/` or a `domain/` can name one without importing
the storage adapter. Its guide names the entry points and the limit; the limit is
asserted rather than described — `kit/fault/fault_test.go` checks the closure
against `go list -deps`, the [package gate](scripts/check_packages.sh) holds the
empty allowance, and `kit/crud` re-exports those same values rather than
declaring copies of them.
SQL transactions, authorization and business writes remain explicit composition
responsibilities; an exported form or rule does not supply a complete service.
The existing CRUD/page/Auth aliases and screens adapter delegate to these owners.
[ADR 0012](docs/adr/0012-independent-parts.md) requires adopted-consumer benefit.
The [package gate](scripts/check_packages.sh) checks transitive runtime imports:
`ui/document`, `ui/resource` and `kit/entity/display` reach no database or HTTP
server, and `ui/page` and `ui/screens` are held to the adapter closure they have
today. The
[version gate](scripts/check_versions.sh) refuses a `replace` directive or a
`go.work` file, so a passing check reflects the versions the module declares;
the [public example](ui/forms/testdata/standalone/README.md) proves ordinary
versioned consumption without the application runtime, and the weekly
[public-consumption workflow](.gitea/workflows/public-consumption.yml) runs it,
because a check that lives in a guide runs when somebody remembers. Neither
proves scale or adoption by an external product.

Product value tiers are a different axis from package boundaries and are not a
layer described here: [ADR 0016](docs/adr/0016-value-tiers-are-acceptance-levels.md)
(accepted 2026-09-19) fixes T1 Capture through T5 Assure as the acceptance
evidence a product must produce for itself — not a package layout, a billing
plan or an implemented capability — and separates the dependency direction those
positions aim at from what the package gate above enforces, which is a recorded
subset. The record qualifies no application: naming a tier authorizes no package
and claims no deployment, and a product reaches a tier with its own evidence.

## Start at the composition

[pkit.Module](pkit/declare.go) declares what it Provides, Needs, optionally uses
and Contributes. An application names its modules with `pkit.NewApp(name).Use(...)`;
the resolver checks selections and orders their builders before infrastructure
opens. Builders supply typed dependencies to implementations and return kernel
manifests. There is one declared composition path, with no runtime discovery.

Missing-provider errors name the consumer and the contract, and the module to
add when the contract's package names one; conflicting providers require Choose.
A cycle error names the first cycle in Use/dependency order and explains its
selected contracts, for example:
`cart → collectibles → cart is a cycle: cart takes cartcontracts.Extension from collectibles; collectibles needs cartcontracts.Service from cart`.
Needs says “needs”, a selected scalar Optional says “uses”, and a contribution
collection says “takes”. Reasons follow declaration order, without duplicates.
An absent optional provider or empty collection adds no edge. Validate, Explain,
Describe and Build use the same refusal; a refused Describe exposes problems
without a partial module plan. [The executable cases](pkit/compose_test.go)
and [Describe parity](pkit/describe_test.go) exercise that contract.

[apps/platformkit/app.go](apps/platformkit/app.go) is the same application read
as one sentence — the modules it uses, the ports the kernel asks the application
for, and the roles it says a tenant begins as — and what it resolves to is
committed beside it as
[COMPOSITION.development.md](apps/platformkit/COMPOSITION.development.md), which
`TestCompositionFile` compares on every `make check` and refuses when the
composition moves and the file does not.

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
`POST /api/v1/public/auth/register` path then requires a full name, password, matching
confirmation and accepted terms. It writes the pending account directly and
returns the same acknowledgment for an existing email. Every attempt hashes
the supplied password before one insertion attempt; no account lookup decides
the public response. An email conflict leaves the transaction usable and cannot
overwrite credentials or roles. Credentials never enter the event outbox.
This mode shares the recovery request limit and needs no email delivery. A
client still composes its terms guidance, pending-success page and review UI.

Password-first mailbox confirmation is a third, exclusive opt-in through
`auth.Deps.EmailRegistration`, with the user registrar and trusted initial roles.
Its registration form accepts the same credentials and consent, stores an
`unverified` account and queues a credential-free event. Auth's worker delivers a
24-hour link at `/auth/verify-email`; the application composes that page, signup,
resend, legal guidance and sign-in. The shared [form controller](ui/assets/js/session.js)
supports `register-password`, `verify-email` and `resend-verification` forms.
Opening a link does not consume it, and confirmation creates no session.

`POST /api/v1/public/auth/verify-email` consumes an auth-owned digest and calls
`VerifyEmail` in one tenant transaction. It checks the current canonical email,
password-bearing unverified state and expiry after waiting for concurrent work;
activation preserves the chosen password and roles. Password setup, recovery and
operator approval cannot satisfy this gate. `POST /api/v1/public/auth/resend-verification`
shares the IP request budget and reserves one request per tenant/mailbox per
minute before lookup, with the same acknowledgment for unknown and cooled
addresses. Recipient-counter failures refuse delivery. Rotation and consumption
share a per-user lock; a new delivered link replaces its predecessor. Transport
errors are sanitized before the existing outbox retry mechanism retains them.

A module has three parts. `contracts/` defines its entities, public service,
events, permissions and conformance suite. `internal/` contains its
implementation. `module.go` declares the constructor and manifest.
[modules/task](modules/task/) is the reference example. The cases a conformance
suite runs are generated by [kit/porttest](kit/porttest/), the test-support
package beside `kit/db/dbtest`: a module describes its port once in
`contracts/<name>test/conformance.go` — its operations, what each refuses,
which cases the port does not owe and why — and the harness runs that one
description against the module's fake and against the real service, so the two
can never answer different questions. The manifest's
subscriptions, jobs and routes are typed by the kernel packages that run them;
[ADR 0013](docs/adr/0013-manifests-name-kernel-types.md) records why
`kit/module` is not a declaration-only leaf and what would move first if a
database-free reader of manifests appeared.

A consumer imports another module's `contracts/`, not its `internal/` or
constructor. Application composition is the place that connects them.
[scripts/check_imports.sh](scripts/check_imports.sh) checks Go's direct production
and test imports. Production contracts have an exact dependency policy, and test
suites composing foreign constructors declare their targets and reason in
`scripts/imports-integration.txt`. A contract dependency the published surface
already carries is recorded per edge, with the frozen exported name that keeps it,
in `scripts/imports-contract-baseline.txt`; a row whose import has gone is refused,
so that list only shrinks. Every module is also placed in one of four layers —
`platform`, `identity`, `generic`, `product` — whose order belongs to the gate
itself, while each repository assigns only its own modules in its own
`scripts/imports-layers.txt`. A production import may not climb that order, and a
declared port prevents such an import rather than excusing it, so the rule carries
no exemption; a pair that imports each other in production states its reason in a
justification row, and a row for a pair that stopped being reciprocal is refused.
[docs/MODULE-GRAPH.md](docs/MODULE-GRAPH.md) is the generated aggregate module
graph, composition edges printed apart from Go imports. The
[import gate](tools/importgate/README.md)
documents provenance, refusals, build context and the per-pair edge report.
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

Services can additionally require a tenant-qualified
[`tenancy.Policy`](kit/tenancy/policy.go) decision after loading current resource
facts. The [Topaz adapter and task example](modules/auth/policies/README.md)
show opt-in assignment and resolution checks shared by HTTP and direct service
callers. Denial and provider failure remain distinct; either refuses mutation.
These checks refine existing grants and domain rules. They do not add policy
filtering to generated lists, directory synchronization or automatic client
adoption.

[kit/db](kit/db/) owns transaction entry and tenant database settings.
`db.Tx[db.Tenant]` and `db.Tx[db.System]` distinguish tenant and system work
in Go. The router imports it for the transaction a request is — `TxFrom`,
the loader and authenticate signatures, the lazy `db.Pending` the middleware
closes — and for nothing that executes SQL of its own; the
[kit/httpx guide](kit/httpx/README.md#the-database-boundary) records why the
pipeline stays there rather than in a second package with the same closure. PostgreSQL row-level security enforces isolation for tenant tables under
the application role. The owner role performs migrations, not ordinary requests.
The tenant settings are PostgreSQL `USERSET` values, so the source gate that
restricts writes to `kit/db` is part of the security boundary, not a substitute
for database privileges. [ADR 0003](docs/adr/0003-tenancy-by-postgres.md)
describes the constraint.

A service records events in its transaction. [kit/events](kit/events/) delivers
the committed outbox through the selected transport and claims each event for
a subscription in the handler's transaction.
[Spec](kit/rest/rest.go) shares its [create, update and delete core](kit/rest/write.go)
between JSON routes and registered resources. Updates and deletes lock the live row before merging,
validation, hooks and event snapshots. This serializes server-side decisions;
it does not reject an old form based on the revision its user originally saw.
Use [rest.Operation](kit/rest/operation.go) for typed service projections with
custom paths or response bodies; it shares transaction and error handling with
commands while the service keeps its domain authorization. Resource counts use
one read guard; standard Specs issue a COUNT without loading entity rows.
[kit/app](kit/app/dependencies.go) defaults to an in-memory transport for the combined
`all` role and JetStream for separate `web` and `worker` roles, unless the
application supplies a transport. The application also supplies the two
constructors (`app.Transports`); the kernel selects by name and imports no
provider, which the [package gate](scripts/check_packages.sh) records for
`kit/app` and `kit/events`. Memory waits for committed handling or a
terminal record before acknowledging publication; unfinished rows recover from
PostgreSQL after a restart. This does not establish the broker deployment's
durability. Terminal recording remains retryable after the handler attempt cap;
[ADR 0004](docs/adr/0004-events-are-the-job-queue.md) defines recovery and retention.

JetStream delivery is at least once. Database claims prevent repeated committed
handling; external effects still need the provider's own idempotency contract.
[kit/jobs](kit/jobs/) schedules work through that event path.

The same walk is measured. [kit/telemetry](kit/telemetry/) names the attributes and
the three instruments and holds no provider, which is what lets the four packages
above make a measurement without reaching a collector; [kit/app](kit/app/) installs
the one `TracerProvider` and `MeterProvider`, OTLP over gRPC, and installs nothing
where no endpoint is configured. Spans cover the boundaries above — the operation,
named after the operation id and opened before routing, the transaction under `Tx`,
the outbox relay (one span for the pass and one per event it hands to the
transport, on that event's own trace), the job run — and the tenant rides on each where the request
resolved one, as a span attribute and never a resource one, because one process
serves many tenants. The publisher's trace context is stored on the outbox row, so a
request and the handler that reacted to it are one trace, and
`pkit.http.operation.duration`, `pkit.outbox.lag` and `pkit.http.refusals` are the
numbers a dashboard would read.

## Every pillar, end to end

Everything before this section walks one request through every layer at once. What
follows takes one capability at a time, in the order a reader meets it, and
answers five questions about each: where it lives and what its one public seam
is, what open standard or library it builds on, how the tenant crosses it and
where that is enforced, how it is traced and audited, and how an application
extends it without rewriting it. Where a capability is only partly on this tree
the paragraph says which part is, and where it opens no span and writes no row
the answer is "nothing". A licence is named as that dependency's own, at the
version `go.mod` pins; the servers this stack runs beside the binary — Postgres,
NATS, Valkey, the object store, the collector — are separate programs, named
where they are configured and never linked.

### Design tokens and components

**Where it lives.** `design`, whose one seam is `Pair` (`design/design.go:167`):
a light `Theme` and a dark one (`design/design.go:26`), with `Theme.Tokens`
(`design/design.go:188`) as the flattened list. Components in `ui/components`
name roles, never colours; a role is a `--pk-role-*` property defined once in
terms of a token, which is the whole reason to have themes (`design/design.go:11-15`).

**Builds on.** Nothing third-party: the package derives from
`github.com/septagon-oss/pk-design`, Apache-2.0, the same holder
(`design/design.go:17`, `NOTICE`), and what it emits is CSS custom properties — a
web standard, not a dependency.

**How the tenant crosses it.** It does not. The theme is chosen once, at
composition (`Theme(design.Default())`, `apps/platformkit/app.go:336`), one
stylesheet serves every tenant, and no theme reads a row: isolation lives in the
database, and a stylesheet adds none of it.

**How it is traced and audited.** Nothing. `Pair` is comparable, so `ui` memoises
one stylesheet per pair rather than recomposing per request
(`design/design.go:165-166`); no span opens and no event publishes.

**How an app extends it.** Hand `ui.Compose` ([ui/ui.go](ui/ui.go)) a `Pair` of its
own, plus a `ui.Extra` ([ui/ui.go](ui/ui.go)) for rules. The reference product's own
line is `apps/platformkit/fault.go:113`: `ui.Compose(design.Default())`, the same
pair the composition hands the shell at `apps/platformkit/app.go:336`. A client's
colours go into that one pair; everything above the tokens is written in terms of
a role.

### CSS and JavaScript

**Where it lives.** `ui/css` is the stylesheet's IR — `Sheet`
(`ui/css/css.go:130`), `AddRule` (`ui/css/css.go:142`) — and `ui/style` compiles
tokens into it (`ThemeVars`, `ui/style/theme.go:24`). Behaviour is controllers
served as static files beside the API ([ui/assets.go](ui/assets.go)), mounted by `ui.Assets`
([ui/assets.go](ui/assets.go)) in `modules/web/internal/mount.go:78` and
`modules/admin/internal/mount.go:146`.

**Builds on.** `maragu.dev/gomponents` v1.3.0 (MIT) for the DOM the controllers
enhance, htmx 2.0.8 (0BSD) and the phosphor icon paths (MIT), both named in
`NOTICE`, and an IR derived from `github.com/septagon-oss/styleengine`,
Apache-2.0 (`ui/css/css.go:11`). Precedence is decided by cascade layers, and
only the client layer is a consumer's (`docs/adr/0018-cascade-layers-decide-precedence.md:1`).

**How the tenant crosses it.** It does not. What is enforced here is instead
mechanical: `scripts/check_ui_layers.sh` (`scripts/check_ui_layers.sh:13-24`)
refuses a raw utility class and a class no rule in its own layer styles, and runs
as `check-ui` (`Makefile:192`). `css.Literal` (`ui/css/css.go:46`) is kernel-only
by decision 0080, and the reason sits above the symbol: nobody outside the binary
contributes CSS (`ui/css/css.go:45`).

**How it is traced and audited.** Nothing: no span, no event, no audit row, and
neither `ui/css` nor `ui/style` imports `kit/telemetry` outside its tests.

**How an app extends it.** A `ui.Extra` sheet feeds rules into the client layer,
which is what a module does for its own prose (`modules/web/internal/mount.go:72`).
A behaviour is a `data-controller` attribute on a source-rendered component
(`ui/components/alert.go:65`, `ui/components/textarea.go:96`) answered in
`ui/assets/js/components.js`, and the page names the controllers it loads
(`ui/document/document.go:39`). A product writes no CSS file and no second
emitter; when the gate refuses a class, the fix is a token or a rule in the same
layer.

### Authorization: OPA

**Where it lives.** [kit/tenancy](kit/tenancy/policy.go) owns `Policy`, `PolicyRequest` and `RequirePolicy`; [Task](modules/task/README.md) is the consumer.

**Builds on.** Embedded OPA uses `github.com/open-policy-agent/opa` v1.21.0;
Topaz uses `github.com/aserto-dev/go-authorizer` v0.24.1 (both Apache-2.0).

**How the tenant crosses it.** [The service](modules/task/internal/service.go) locks under [RLS](docs/adr/0003-tenancy-by-postgres.md)
in `db.Tx[db.Tenant]`; `RequirePolicy` then validates tenant equality and explicit actor identity.

**How it is traced and audited.** Decisions run within [the HTTP span](kit/httpx/traced.go);
[the refusal hook](kit/httpx/authorize.go) records authenticated denials as `security.denied`.
OPA hashes its source; Topaz Revision is configuration, not server freshness.
Refusal silence concerns task domain effects; audit and receipts have separate paths.

**How an app extends it.** [The product](apps/platformkit/product.go) provides `tenancy.Policy` through pkit;
[task.Module](modules/task/provider.go) consumes it optionally. [cloud.profile](pkit/cloud.go) does not select policy.
Deliberate absence retains route grants; configured failure never becomes that absence.

#### Policy substitution boundary

The consumer is `POST /api/v1/task/tasks/{id}/resolve`, with `assign` beside it.
HTTP checks account and `task:update` in the request transaction; task locks and asks policy before domain checks/retries.
`task:assign`/`task:resolve` are policy actions. Direct service callers own current account/grant checks.

| Dimension | Supported boundary | Evidence and limits |
|---|---|---|
| Inputs | [Task facts](modules/task/internal/policy.go): transaction tenant ID/slug, principal user ID, action, locked resource kind/ID/tenant and status/priority/assignee_id; assignment adds requested_assignee_id. | OPA receives tenant/actor/action/resource directly; Topaz nests them under input.resource, adds tenant.operator/resource.tenant_id and sends MANUAL identity (NONE for public). OPA omits those two extra fields; Task uses user actors/string attributes only. |
| Decisions | RequirePolicy rejects invalid facts before evaluation and discards any decision accompanying an error. Denial is 403 POLICY_DENIED; invalid facts/provider failure are sanitized 503 POLICY_UNAVAILABLE. | Denial is distinct from outage; a fresh call evaluates current policy and resource facts. A command refused by policy returns no task and changes no task state or domain event. |
| OPA | [The adapter](kit/tenancy/providers/opa/opa.go) evaluates compiled rules per call, with caller context, no default timeout or decision cache; undefined denies, malformed/conflicting results fail. | [Library tests](kit/tenancy/providers/opa/opa_test.go) and [reference HTTP journey](apps/platformkit/object_scope_test.go) exist. Direct Decide validation parity with Topaz remains unverified; the consumer uses RequirePolicy. |
| Topaz | [The adapter](kit/tenancy/providers/topaz/topaz.go) requires one correctly named boolean decision; RPC/encoding errors or missing/duplicate/wrong decisions fail. No local decision cache; timeout defaults to two seconds, with earlier caller deadlines honored. | [Real client/protobuf tests](kit/tenancy/providers/topaz/topaz_test.go) use a disposable gRPC protocol double, not a Topaz service certification. Application owns connection/close, deployment and directory synchronization. Server caches/freshness remain unverified. |
| Local ports | Task Service.Assign/Resolve/CheckSLA, Creator.Create, Writer.Save, LockedReader.TaskForUpdate and Reader.TaskByID share the caller's local tenant transaction; the plain reader has no write lock. Mutations/outbox commit together. | [Contracts](modules/task/contracts/task.go) support local SQL participation, not a remote clone of db.Tx. Row locks serialize task commands; no expected-revision argument exists. Remote allow and concurrent grant/directory revocation are not atomic with local commit. |
| Fresh retries / cancellation | A fresh invocation reloads locked facts and evaluates again before returning an idempotent result. Cancelled or expired evaluation refuses without task effects; OPA checks context before and after evaluation. | [Common consumer cases](modules/task/contracts/tasktest/policy_conformance.go), [scope probes](modules/task/provider_scope_test.go), [cancellation](modules/task/provider_cancellation_test.go) and [SQL boundaries](modules/task/provider_transaction_test.go) exercise both adapters. Cancellation racing COMMIT can leave an uncertain outcome; it does not prove rollback. |
| Cached answers | [HTTP recovery](kit/httpx/operation_receipt.go) can bypass the task handler; route grants still run. | Task declares no [RecheckingReplay](kit/httpx/auth.go) policy hook. The [reference-app characterization](apps/platformkit/policy_recovery_test.go) confirms cached replies bypass policy revocation for both adapters, while revoked route grants refuse them. This is an unsupported authority guarantee, not a safe retry claim. |
| Other substitutions | The task row/outbox are local effects; external policy is a bounded remote decision. | Other providers, arbitrary attribute types, remote transaction services and deployed directory behavior remain unverified. |

The [common suite](modules/task/provider_consumer_test.go) runs the same cases against the existing fake, SQL service and mounted HTTP commands.
It uses real embedded OPA and the official Topaz client over a disposable gRPC protocol double; no Topaz process is certified.
The fake carries the service's trusted attributes and shares [domain.Resolve](modules/task/domain/resolve.go), but proves no RLS/rollback. Compare adapters under equal rules:
the [Topaz example](modules/auth/policies/README.md) allows self-claim; the reference rule allows reassignment/unassigned resolution.
`Choose` selects whole modules. The reference fixture supplies its policy through the same product declarations at construction,
retaining the product's other provisions; it adds no cloud-profile policy selector or runtime reload.

### Authentication: sessions, TOTP, and WebAuthn

**Where it lives.** `modules/auth`: `Session` and its table
(`modules/auth/contracts/auth.go:105`, `:127`), and `Factors` as the seam for
anything beyond a password (`modules/auth/contracts/factors.go:147`) over a
`Factor` that carries its `Kind` (`:114`) — `totp` or `passkey` — with the
ceremony half beside it as `Passkeys` (`modules/auth/contracts/passkeys.go:90`),
because the enrolment pair `Factors` offers is shaped by a secret the server
hands out and nothing in that shape is a parameter of a ceremony. The browser's
half is `kit/httpx/cookies.go` — `SessionCookie` (`kit/httpx/cookies.go:22`),
`CookieName` (`:43`), `SessionCookieOf` (`:63`).

**Builds on.** RFC 6238 in one file over `crypto/hmac`
(`modules/auth/internal/totp.go:23`): RFC 4226's 160-bit seed (`:41`), the digits,
step and skew fixed in the contracts (`modules/auth/contracts/factors.go:81-88`),
RFC 4648 base32 without padding (`modules/auth/internal/totp.go:45-46`), a
constant-time comparison over a window walked in a fixed order (`:114`), and the
`otpauth:` URI an app scans (`:133`). The file says why there is no library: a
wrapper around those thirty lines would move the review rather than remove it
(`modules/auth/internal/totp.go:28-29`). The
dependencies are `golang.org/x/crypto` (BSD-3-Clause), whose `nacl/secretbox`
seals the stored secret, `github.com/coreos/go-oidc/v3` (Apache-2.0) for OIDC,
and `github.com/go-webauthn/webauthn` (BSD-3-Clause) for the assertion — whose
only non-test importer is `modules/auth/internal/passkeys.go`, the soft
authenticator that exercises it being a test fixture that speaks the SDK's own
CBOR at that seam (`modules/auth/internal/passkey_authenticator_test.go`), so
CBOR and COSE reach no handler, no event and no contract
(`modules/auth/README.md:220-222`).

**How the tenant crosses it.** `sessions.tenant_id` under row-level security
(`modules/auth/migrations/000008_auth.up.sql:15`, `:41-48`) and the same for
`totp_factors` and `recovery_codes`
(`modules/auth/migrations/000031_auth_factors.up.sql:22`, `:47`, `:64`). The
secret column is sealed rather than hashed, because verification needs the value
and a backup must not carry a spendable factor (`:24-31`), and only the
deployment's key opens it — `svc.EnableFactors([]byte(deps.FactorKey))`,
`modules/auth/module.go:270`. Without a key the TOTP and recovery-code doors
answer 503 rather than the routes going missing (`Deps.FactorKey`, `:120`). The
passkey tables carry the same column and the same policy —
`passkey_credentials`, `passkey_challenges`, and the usernameless door's own
`passkey_settings`
(`modules/auth/migrations/000035_passkeys.up.sql:20`, `:76`, `:144`, `:103`,
`:109`, `:152`) — and nothing in them is sealed, because a public key is public
and those doors mount whether or not a factor key exists
(`modules/auth/module.go:272-283`).

**How it is traced and audited.** Enrolment and withdrawal are events —
`auth.factor_enrolled` and `auth.factor_withdrawn`
(`modules/auth/contracts/events.go:174`, `:179`), with `auth.session_revoked`
beside them, and the passkey half adds the two quieter ones: `auth.factor_used`
names which factor answered a door (`:194`), and `auth.factor_suspect` an
assertion whose counter went backwards (`:200`). The trail holds them because
the audit module subscribes to every declared name
(`modules/audit/module.go:93`). No span of its own: the sign-in request is the
router's.

**How an app extends it.** `Factor.Kind` is the opening, and the second kind is
here: a passkey answers the second factor, `github.com/go-webauthn/webauthn`
plus its CBOR/COSE tree priced as a module dependency in its own `build(budget)`
commit, written down beside what stays out
(`modules/auth/README.md:213-215`, `:248-251`). What the library leaves to
the relying party is decided where it is enforced: the config is
built per request from the host that already chose the tenant, so a credential
minted at one tenant's host cannot answer at another's (`:205-208`), and whether
a passkey may be the whole sign-in, with no password offered first, is that
tenant's own row in `passkey_settings` read per request rather than a route an
operator holds (`:211-215`). The refusals a person hits are correctable — a step
replayed after it was spent
(`modules/auth/internal/totp_step_spend_refuses_a_replay_of_the_same_step_test.go`),
and a second factor offered without its first
(`modules/auth/internal/second_factor_requires_its_first_half_test.go`).

### Locales: kit/locale

**Where it lives.** `kit/locale` — `Messages` (`kit/locale/locale.go:9`),
`Formatter` (`:16`), `Locale` (`:21`), `SelectLocale` (`:29`) — with one
adapter, `kit/locale/providers/xtext`: `Load` builds a catalogue from ordered
sources (`kit/locale/providers/xtext/load.go:33`), `Catalog` and `Source`
describe what goes in (`:107`, `:114`), and `FromCatalog` hands back a
`locale.Messages` (`kit/locale/providers/xtext/catalog.go:16`).

**Builds on.** `golang.org/x/text` v0.43.0 (BSD-3-Clause) for language matching
and message catalogues (`kit/locale/providers/xtext/catalog.go:7-9`) — BCP 47
matching and CLDR plural rules. Nothing here is a service.

**How the tenant crosses it.** `tenancy.Languages` (`kit/tenancy/tenancy.go:53`)
hangs off the resolved tenant (`:44`) and off the resolved host
(`kit/httpx/tenant.go:49`, `:53`), and each request negotiates inside what that
tenant declared: `page.Serve` selects from `TenantPreferences`
([ui/page/serve.go](ui/page/serve.go), `ui/page/locale.go:61`). Why a refusal may not answer in
an undeclared language is the comment at `kit/httpx/tenant.go:262-275` — a page
one of them renders would negotiate from the caller's `Accept-Language` alone and
answer in a language this tenant refused. The declaration is a row
(`migrations/000029_tenant_locale.up.sql:1`).

**How it is traced and audited.** Nothing: `kit/locale` imports no
`kit/telemetry`, and selecting a locale writes no span and no audit row.
`e2e/localization.spec.ts` is what proves the promise held in a browser.

**How an app extends it.** Append an `xtext.Source` to the ordered list
`catalogues()` builds (`apps/platformkit/catalog.go:40`); the order *is* the merge
rule, and the comment above that function says why each source sits where it does
(`apps/platformkit/catalog.go:26-39`). Owner modules ship their own
`messages/<tag>.json`; `modules/notification` deliberately ships none, because its
words belong to the module that raised the notice. A reader who asks in a language
its tenant never declared is not served in that language and is not refused for
asking: the preferences the tenant does not serve are dropped and the tenant's own
default answers (`ui/page/locale.go:104-113`, the case at
`ui/page/tenant_locale_test.go:131-140`). Being served in the caller's language is
the tenant's declaration to make — `scripts/e2e.sh:202-208` shows bootstrap's
`--language` doing exactly that.

### Change control: modules/change

**Where it lives.** `modules/change`: `Proposal`
(`modules/change/contracts/change.go:82`), `Service` (`:210`), `Subject` (`:254`)
and `SubjectBinding` (`:267`) as the seams, `Deps` (`modules/change/module.go:32`)
as the wiring.

**Builds on.** Nothing third-party. This is a protocol with a person in it, and
its only dependency is the kernel.

**How the tenant crosses it.** `change_proposals.tenant_id` under `ENABLE` and
`FORCE` row-level security (`modules/change/migrations/000038_change.up.sql:13`,
`:74-75`), inside a uniqueness key that starts with the tenant (`:65`).

**How it is traced and audited.** Two trails, and the distinction is the point.
The *write* is the owner's: an accepted proposal is applied through the subject's
`Save` (`modules/change/contracts/change.go:254-266`), which the composition
implements over the owner's own service (`apps/platformkit/change.go:157-165`),
so the domain audit row is the owner's event — `site.settings_updated` for the
reference subject (`modules/site/contracts/events.go:15`). The
*decision* is this module's, one event per transition and no fifth name
(`modules/change/contracts/events.go:11-19`), published beside the row that moved
(`modules/change/internal/service.go:307`) and so audited the way every declared
event is (`modules/audit/module.go:89-93`). `Applied` carries the subject's
revision before and after, so the trail alone answers "this change produced that
revision" (`modules/change/contracts/events.go:59-73`), and the reference product
waits for `change.proposal_applied` in the trail and checks whose name it credits
(`apps/platformkit/change_control_test.go:279-285`). Neither trail stands in for
the other, and a save that fails publishes nothing: the write errors out before the
event is built (`modules/change/internal/service.go:296-300`). No span of its own.

**How an app extends it.** Implement `Subject` and bind it: `siteSubjectBinding`
(`apps/platformkit/change.go:185`) and the one line that calls it
(`apps/platformkit/product.go:334`) are the whole of what the reference product
adds. A refusal carries the owner module's answer in `Refusal`
(`modules/change/contracts/change.go:284`), which is why this module needs no
vocabulary of its own.

### Events: CloudEvents, the outbox, JetStream

**Where it lives.** `kit/events` — [`Publish` and `PublishFor`](kit/events/events.go)
record tenant and system writes; [`Consume`](kit/events/consume.go) delivers them
through the [claim and dead-letter paths](kit/events/events.go). The
wire format is its own file, `kit/events/transport/cloudevents.go`. `Observe`
(`kit/events/transport/transport.go:174-185`) is the other door onto the same stream:
one tenant's one event, watched from now on, claiming nothing, retrying nothing
and holding no history — what a live connection is built on (`kit/stream`), and
what an app opens for its own events by declaring `Streams`
([Module.Streams](kit/module/module.go)).

**Builds on.** CloudEvents 1.0 in structured content mode, hand-built: the
envelope (`kit/events/transport/cloudevents.go:41`) is what crosses a broker, so
a bridge, an event router or an AsyncAPI document can read a PlatformKit event
without importing this package (`:3-7`). `SpecVersion` is `"1.0"` and nothing
else is accepted (`:27`). The broker is NATS JetStream,
`github.com/nats-io/nats.go` v1.54.0 (Apache-2.0), from
`kit/events/providers/nats` (`Connect`, `kit/events/providers/nats/options.go:14`)
against the `nats:2-alpine` container (`compose.yaml:52`).

**How the tenant crosses it.** Three ways, all load-bearing: `Publish` takes a
`db.Tx[db.Tenant]`, so no call publishes without a tenant transaction
([publication](kit/events/events.go)); the `tenantid` extension is required here, because an
event with no tenant has no transaction to deliver it in
(`kit/events/transport/cloudevents.go:50-54`); and the outbox rows are filtered by
row-level security (`migrations/000002_outbox.up.sql:15`, `:37-38`).

**How it is traced and audited.** W3C trace context travels verbatim as `traceparent`
and `tracestate` (`kit/events/transport/cloudevents.go:56-62`), plus a PlatformKit-
owned `baggage` carrying the publisher's request id, because the specification fixes
those two members and says nothing about correlation (`:63-70`; the column is
`migrations/000041_outbox_baggage.up.sql:42`). Spans: the relay's batch
([relay batches](kit/events/relay.go)), `<event> publish` and `<event> deliver`
(`kit/events/trace.go:109`, `:134`). The trail *is* this stream: `SubscribeAll`
(`modules/audit/module.go:93`) turns every declared event into a row.

**How an app extends it.** Declare in the manifest — `Declared` and
`Subscriptions` ([Module](kit/module/module.go)) — and write one transport line
at composition: `app.Transports{Memory: memory.New, JetStream: eventnats.Connect}`
(`apps/platformkit/product.go:382`), in the function the application calls the one
place it names an event provider (`:381`). `kit/app` selects by name and imports
no provider ([Transports.constructor](kit/app/app.go) and
[App.transport](kit/app/dependencies.go)). An unknown `specversion` is refused rather
than guessed at, and an event with no tenant cannot be formed; a claimed event
that is already handled is a no-op, so delivery is at-least-once and an external
effect still needs its provider's idempotency contract.

### Notifications

**Where it lives.** `modules/notification`: `Notification`
(`modules/notification/contracts/notification.go:29`), the `Notice` a refusal
raises (`:72`), `RecipientLookup` (`:116`) and `HostLookup` (`:133`) as what it
needs from other modules, `Mailer` (`:179`) as what it does not, and `Service`
(`:186`). It ships no `rest.Spec`, and the manifest says why: a Spec's list route
is the whole tenant, and these rows are one person's (`modules/notification/module.go:5`).

**Builds on.** `github.com/wneessen/go-mail` over SMTP (`gomail.New`,
`modules/notification/providers/gomail/gomail.go:70`) or the module's own mailbox
(`modules/notification/mailbox.go:30`), one Go template, no message catalogue.

**How the tenant crosses it.** `notifications.tenant_id` under `ENABLE` and
`FORCE` with the tenant-match policy
(`modules/notification/migrations/000011_notification.up.sql:13`, `:36-41`), plus
the delivery ledger in `000027_notification_deliveries`. An address and a host
arrive only through the two ports the application implements over the user and
tenant modules, so this module never names either
(`modules/notification/provider.go:29-30`).

**How it is traced and audited.** Its ten events
(`modules/notification/contracts/events.go:20-52`) reach the trail through
`SubscribeAll`; no span of its own.

**How an app extends it.** Implement `Mailer`. The deployment names which sender
it has (`FromDeployment`, `modules/notification/provider.go:40-43`): SMTP when the
process names a host, port and from-address, the mailbox when not (`:60-62`), and
the mailbox is no stub — "it keeps every message and logs each one" (`:24-26`) —
so a deployment without mail records everything and says what it would have sent
(`apps/platformkit/main.go:98-104`). A refusal a person must see raises a
`Notice` (`apps/platformkit/product.go:473`). A nil lookup writes every row and sends no
mail: the notice is the record, the mail is a copy.

### Object storage: modules/file over S3

**Where it lives.** `modules/file`: `Scope` (`modules/file/contracts/scope.go:39`)
with `ScopeOf` (`:42`), `ScopeOfTx` (`:53`) and `Scope.ObjectName` (`:92`) as the
seam between a tenant and a name; `Deps` and `S3Config`
(`modules/file/module.go:71`, `:56`) as the wiring; and two adapters,
`modules/file/internal/s3.go` (`NewS3`, `:68`) and `modules/file/internal/local.go`.

**Builds on.** `github.com/minio/minio-go/v7` v7.3.0 (Apache-2.0) over the S3
API, which the module names as several stores — AWS S3, Garage, SeaweedFS, Ceph
RGW (`modules/file/module.go:43`); locally it is tested against the SeaweedFS
image pinned by digest (`compose.yaml:122`). The client library is the dependency;
the store is a separate program.

**How the tenant crosses it.** In the object name: `Scope.ObjectName` produces
`<tenant uuid>/<key>` (`modules/file/contracts/scope.go:92`,
`docs/adr/0019-the-tenant-is-in-the-object-name.md:1`), so one bucket holds every
tenant's bytes with no bucket policy doing the separating
(`modules/file/README.md:38`). The rows carry `tenant_id` under `ENABLE` and
`FORCE` with the tenant policy (`modules/file/migrations/000019_file.up.sql:11`,
`:46-51`).

**How it is traced and audited.** No span of its own. Upload, delete, retention,
release and erasure are its five events
(`modules/file/contracts/events.go:23-27`) — one work order per blob, so a
delivery that dies on the eleventh retries the eleventh (`:15-21`) — and
`apps/platformkit/erasure_audit_test.go` checks the trail holds them.

**How an app extends it.** Write `S3Config` into `Deps.Storage` or let `Local`
stand (`modules/file/module.go:53`), and hand the store to another module the way
this repository hands it to content: the file module puts its own rich-text
adapter (`modules/file/provider.go:49`) and content mounts it
(`modules/content/module.go:82`). Over quota is correctable — free space, or ask
for quota (`modules/file/module.go:83-86`); a store that cannot prove erasure is
not, at that adapter: S3 leaves `verified_at` NULL and leans on a bucket lifecycle
rule (`:51-52`).

### Cache: kit/cache over Valkey

**Where it lives.** `kit/cache`: `Scope` (`kit/cache/cache.go:87`), built only by
`Of` (`:101`) or `Shared` (`:117`), `Key` (`:143`), `Cache` (`:175`) and `Value`
(`:243`); the adapter is `kit/cache/providers/valkey` (`New`
`kit/cache/providers/valkey/valkey.go:57`, `Connect` `:71`). What may be cached at
all is the package comment: values a replica may recompute, and never sessions,
permission grants or entitlements (`kit/cache/cache.go:12-14`).

**Builds on.** `github.com/redis/go-redis/v9` v9.22.0 (BSD-2-Clause) against
`valkey/valkey:8-alpine` run with `--maxmemory-policy noeviction`
(`compose.yaml:104-105`). The kernel imports neither a Valkey server nor a
Valkey-specific client, and `docs/cache.md` holds the rules.

**How the tenant crosses it.** The key *is* the tenant: `CacheKey` spells
`<app>:<tenant>:<name>` (`kit/cache/appname.go:47`) and takes no shortcut in the
middle, because a uuid is thirty-six fixed characters, so no name can shift an
earlier segment of the key (`:42-46`). `Scope` keeps the tenant unexported so a
caller cannot forge one, and every request warms the shared host index through
`cache.Shared` (`kit/httpx/tenant.go:34`).

**How it is traced and audited.** Nothing: a cache read opens no span and writes
no audit row, `kit/cache` imports no `kit/telemetry`, and the cache is never a
second source of truth. `kit/cache/cachetest/conformance.go` is the suite both the
in-process store and the Valkey adapter run, over two fixed tenants
([tenant fixtures](kit/cache/cachetest/conformance.go)) so a key that lets one customer reach
another is reproducible from the failure message alone.

**How an app extends it.** Add a field to [app.Caches](kit/app/app.go),
selected by [validateDeployment](kit/app/construct.go); the reference composition names `valkey.Connect` and nothing
else (`apps/platformkit/product.go:409`, in `caches()` at `:408`). A selected
adapter with no constructor is refused before anything opens
([Caches.constructor](kit/app/app.go)). `noeviction` is a requirement and not politeness: the
counter a `Move` raises carries no TTL, an evicted counter reads back as
generation 0, and what `Move` closed reopens (`compose.yaml:99-103`) — a deployment
fault, not a caller's.

### Metrics and traces: OpenTelemetry

**Where it lives.** `kit/telemetry` names the attributes and the instruments and
holds no provider: `Scope` (`kit/telemetry/telemetry.go:31`), `Tracer` (`:47`),
the attribute names (`:53-56`), `WithRequestID` and `RequestID` (`:85`, `:102`),
`SpanAttrs` (`:109`), `MetricAttrs` (`:137`), `RefusalClass` (`:161`), and
`Instruments` (`kit/telemetry/instruments.go:34`). `kit/trace` carries the
context — `Context` (`kit/trace/trace.go:56`), `Parent` (`:67`), `Parse` (`:97`) —
with the header names written once (`:24-28`). `kit/app` installs the one
`TracerProvider` and `MeterProvider` (`kit/app/telemetry.go:302`, `:303`).

**Builds on.** `go.opentelemetry.io/otel` v1.46.0 (Apache-2.0) and its OTLP/gRPC
exporters; W3C Trace Context, which `kit/trace` implements rather than wraps. The
collector is a separate program (`compose.yaml:86`).

**How the tenant crosses it.** Every span and number is stamped from the context:
`SpanAttrs` puts `pkit.tenant` — the slug a person recognises — and
`pkit.tenant.id` on a span, with `pkit.request.id` on spans only
(`kit/telemetry/telemetry.go:53-56`, `:109`), and `MetricAttrs` is the same list
without the request id, because a value unique per request makes one series per
request (`:137`, `:123-135`). The tenant is never a resource attribute: one
process serves many tenants, as `## Follow a request` says.

**How it is traced and audited.** This pillar *is* the trace: the transport span
(`kit/httpx/traced.go:44`), named after the operation id (`:80`), the transaction
span (`kit/db/tx.go:284`), publish and deliver (`kit/events/trace.go:109`, `:134`),
the relay batch ([relay batches](kit/events/relay.go)), and the job run with one span per
tenant (`kit/jobs/jobs.go:194`, `:313`). The audit row stores `traceparent`
verbatim and indexes the trace out of it, as the audit section below explains.
The instruments are the three named at `kit/telemetry/instruments.go:61-69`,
with latency buckets that run to a minute because the work slow enough to matter
is slower than the SDK's defaults (`:22-25`). `kit/telemetry/README.md` holds
the measured figures; this page quotes none.

**How an app extends it.** A module never holds a provider. A deployment writes
`telemetry.otlp_endpoint` and `telemetry.sample_ratio`
([config.Telemetry](kit/config/config.go)), whose empty endpoint exports
nothing ([planTelemetry](kit/app/telemetry.go)); any package names attributes
through `SpanAttrs`. An unreachable collector must not stop serving; the
[telemetry plan](kit/app/telemetry.go) keeps export failures out of request handling.

### Audit

**Where it lives.** `modules/audit`: `Event`
(`modules/audit/contracts/audit.go:53`), whose `TableName` pins `audit_events`
(`:87`), `Query` (`:108`) and `Service` (`:125`);
`PermissionAuditRead = "audit:read"`
(`modules/audit/contracts/permissions.go:9`) gates a read, and `Deps`
(`modules/audit/module.go:45`) is the wiring.

**Builds on.** Nothing third-party. Its one design dependency is the event
stream: a module is audited by having emitted an event
(`modules/audit/contracts/audit.go:1-10`).

**How the tenant crosses it.** `audit_events.tenant_id` under `ENABLE` and
`FORCE` with the tenant-match policy
(`modules/audit/migrations/000010_audit.up.sql:16`, `:40-45`), and a read takes
`audit:read`. `migrations/rls_test.go:30` imports the module and `:45` lists every
owner's migrations, so the claim covers every table this repository creates.

**How it is traced and audited.** `request_id`, `client_ip` and `traceparent` are
carried on the event and stored
(`modules/audit/migrations/000035_audit_context.up.sql:34-36`), the last one
verbatim as the standard spells it (`:18`), and indexed for the two questions a
reader asks: every row this request wrote
(`modules/audit/migrations/000036_audit_request_index.up.sql`) and every row this
trace touched (`modules/audit/migrations/000037_audit_trace_index.up.sql`). The
trace id is read out of that one stored string rather than stored twice beside it
(`modules/audit/contracts/audit.go:76-86`), and `client_ip` is the peer address of
the connection, never a header a client could write (`:63-64`). Say what that does
not include: the trail is append-only in this module's code, it is not
hash-chained, and the application role still holds `UPDATE` and `TRUNCATE` through
the cluster's default privileges (`modules/audit/contracts/audit.go:16-27`,
restated at `modules/audit/README.md:51-53`). No hash chain is on main.

**How an app extends it.** Nothing per app, and that is the point:
`SubscribeAll: true` (`modules/audit/module.go:89-93`) is expanded by the kernel
into one subscription per declared event after every manifest is read, so a module
added later is audited by having emitted an event and by nothing else. Retention is
a `Deps` field rather than a constant, because which retention a deployment owes is
the composition's decision (`modules/audit/module.go:50-61`).

### End-to-end and flows

**Where it lives.** `e2e/` — 27 specs, one Playwright config
(`e2e/playwright.config.ts:10`) and shared steps in `e2e/steps/content.ts` —
behind the `e2e` goal (`Makefile:131`) and `scripts/e2e.sh`.

**Builds on.** `@playwright/test` ^1.63.0 (Apache-2.0), Chromium only, one worker,
no retries: a gate, not a compatibility matrix
(`e2e/playwright.config.ts:6-8`). It is a devDependency that drives a browser, not
a dependency of the runtime.

**How the tenant crosses it.** Each run boots the application against a database
of its own, migrated from nothing, and creates one tenant and one administrator
with `platformkit bootstrap --tenant e2e --host localhost`
(`scripts/e2e.sh:206-208`); the browser then arrives at that tenant's host.
Playwright starts nothing, because the teardown has to happen whichever step
failed (`e2e/playwright.config.ts:4`, `scripts/e2e.sh:11-12`).

**How it is traced and audited.** The specs assert refusals and rendered state,
and that every table is scoped to its tenant is proved by `migrations/rls_test.go`,
which walks every table the kernel's and every reference module's migrations
create (`migrations/rls_test.go:45`). No span belongs to this pillar: it is the
pillar that checks the others.

**How an app extends it.** Add a spec, and share steps through
`e2e/steps/content.ts`. What this pillar's settings buy is a failure that repeats,
not an assertion that means something: one worker and `retries: 0` make a passing
spec a pass seen on the first attempt (`e2e/playwright.config.ts:12-14`), and a
database created for the run and dropped after it (`scripts/e2e.sh:148`, `:120`) make
the state it found the state the run created. That the spec expects the right thing
is the spec's own claim to carry — it names the refusal or the rendered state it
wants, and the refusals themselves are proved by the command tests, not here. A
flaky spec is a bug report, not a retry.

### Mobile

**Where it lives.** The contract is a document. `ui/screens` publishes the screen
and operation catalogue — `CatalogVersion` (`ui/screens/catalog.go:25`), `Catalog`
(`:31`), `Entry` with the operation ids an app calls (`:42`, `:77`) — at
`/api/v1/app/resources` ([surface addresses](kit/httpx/surfaces.go)); `kit/app` renders the event
half as AsyncAPI 3.0.0 (`kit/app/asyncapi.go:1`, `AsyncAPI` `:41`, `CoveredEvents`
`:169`), pinned by the golden files in `apps/platformkit/testdata/`.
`apps/platformkit/wire_compatibility_test.go` is the gate, with the rules B1–B6
that `kit/wire` owns and lists (`kit/wire/README.md:28-35`) over the whole
authorization declaration an operation emits, kind, permission and plan feature
(`kit/httpx/auth.go:136-142`), and
`e2e/maestro/flows.json` is the device journey behind
`mobile-e2e` (`Makefile:141`) and `.gitea/workflows/mobile.yml:1`.

**Builds on.** OpenAPI 3, emitted by `github.com/danielgtaylor/huma/v2` v2.39.1
(MIT), and AsyncAPI 3.0.0 as a document this repository writes itself. Maestro
runs the journey as a separate program (`scripts/mobile_e2e.sh`), so it is not a
dependency. Screens are derived from schemas rather than authored
(`docs/adr/0007-screens-are-derived-from-schemas.md:1`).

**How the tenant crosses it.** The document is reached behind host resolution, on
the tenant's own address, so a shell never names a tenant
([surface addresses](kit/httpx/surfaces.go)), and every operation inside it carries the tenant
through the request's transaction like any other call. `CoveredEvents`
(`kit/app/asyncapi.go:169`) counts the declared events the document names, which
is the claim that a shell sees no event nobody declared.

**How it is traced and audited.** Nothing new: the events a shell may see are the
events the events section traces and the audit section records, on the same
trace. The documents are produced at boot and their bytes are pinned, so a
renamed JSON tag fails `make check` rather than a shipped build.

**How an app extends it.** Nothing — a shell renders the document, and a
product's share is not to extend it but not to break it. Nothing overrides a
refusal, including `UPDATE_GOLDEN=1` (`kit/wire/README.md:17-19`); a planned
breaking change ships as a new
address plus an alias row (`kit/httpx/aliases.go:62`,
`httpx.API.Alias` and `module.Module.Moved`). B1–B6 are immutable to the change —
a removed operationId, a retyped schema member, a new required field, an enum that
strands a reader, a narrowed authorisation, a plan feature added, dropped or renamed —
and additive change is always allowed.
`apps/platformkit/catalog_version_test.go` is the companion proof that
the version number moves when it must.

## Evolve the schema by owner

The foundation and each selected module supply ordered `db.MigrationSource`
values. Each source has a stable owner and its SQL filesystem. `kit/app`
collects the sources in composition order; there is no global version range
or flattened migration filesystem.

[migrations/](migrations/) is now the kernel's own schema: the tenancy helper
functions, the tenants and hosts they resolve, the outbox with its claims and
dead letters, and the limits ledger. Everything a module stores lives beside
that module — `modules/<name>/migrations`, embedded and exported as
`<module>.Migrations`, with the manifest handing the kernel the same files. A
test composes the schema it needs by naming those sources:
`dbtest.Schema(t, user.Migrations, auth.Migrations)`.

The schema a module's objects live in is named exactly that module's owner, and
the kernel's `platformkit_module_schema` (`migrations/000026_module_schema.up.sql`)
is the one line that opens it to the roles the deployment hands its own default
privileges to in that namespace, without any module SQL naming a role. No table
moves here: all nine reference modules that
own SQL still create their tables unqualified, so they land in whatever schema
the runner's `search_path` names — `public` in the reference deployment, the
test's own schema under `dbtest` — and so do the kernel's. A module takes its own
schema in its own later revision, and none has yet. The check that every table is
scoped to a tenant runs over `dbtest.TenantTablesSQL`, which follows the ledger's
owners rather than one schema, so a table is inside the walk, and reported as
`schema.table`, as soon as it has a schema of its own.

The files kept the version numbers they carried when the foundation applied
them, and each module declares `Adopts` for them, so an installation migrated
before this split is re-owned by checksum inside the migration transaction and
no SQL runs twice. `db.Adoption` is that declaration; a fresh database has
nothing to adopt and reads the same ledger either way. An adopted file is an
applied file, so changing one still refuses.

The runner validates the selected source files, obtains its namespace's advisory
lock, checks applied histories, and executes each pending file with its history
row in one transaction — which is the mode a file declares in its header, and two
modes say otherwise: an `autocommit` file's one statement runs outside the
transaction, and a `phase=data` file's body runs once per window of its table's
primary key, each window committed on its own and drained by the worker rather
than by a boot. [migrations/README.md](migrations/README.md) carries that grammar
and the rules the runner refuses by.
Applied files are immutable. A failed file rolls back; completed earlier files
remain applied. Disabling a module retains its data and migration history.

[ADR 0011](docs/adr/0011-migration-ownership.md) defines accepted SQL, integrity
checks, the clean baseline and upgrade behavior. Append a revision to repair a
schema; do not edit the history table to make a failed migration appear applied.
An older image is not a schema rollback. A rolling release requires evidence
that both running versions can use the schema, and the evidence a schema change
carries is a rehearsal: `make rehearse` applies this tree's pending files to a
copy of a production-shaped database — the previous release's own migration, plus
seeded rows, or an operator's dump — and reports the duration the runner measured
for each file against the budgets an operator names.
`platformkit migrate` is the same migration without a server attached, for the
file that came back contended and may be run again.

## Compose the interface

[design](design/) owns theme values and typography. A `design.Pair` supplies
light and dark themes. Each theme's optional `Typography` selects display, body
and mono fallback stacks; empty fields retain the defaults. Supply the same value
on both themes for shared type, and deliver licensed font assets separately.
Components name semantic roles, roles resolve to tokens, and themes supply values.
[style.ThemeVars](ui/style/theme.go) renders both themes' tokens as custom
properties; `design` emits no CSS and depends on the standard library alone,
which the [package gate](scripts/check_packages.sh) enforces.
[ui/README.md](ui/README.md) maps the presentation packages and the rules the
gates enforce between them.

A client that supplies values instead of copying a palette writes a seed.
[design.FromSeed](design/seed.go) generates both themes from a sector, a name and
an optional brand colour, and refuses the result below the contrast
[design.Contrast](design/contrast.go) measures; [kit/designconfig](kit/designconfig/)
decodes `clients/<slug>/design.yaml` into that pair, keyed by slug so one process
can hold many. Two gates decide the result, because a theme is not what is painted:
`Theme.Check` measures the `--pk-color-*` tokens a theme sets, and
`Theme.CheckRoles` measures the `--pk-role-*` layer a component paints, over the
pairs [ui/style](ui/style/README.md) lists. See [design/README.md](design/README.md).

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

[export.ExportTokens](ui/export/export_tokens.go) composes the existing colour, fallback
family, scale, shadow and timing owners without components or I/O. Select
`light`, `dark` or both explicitly; output follows selector order. Theme token
kinds other than colour and font family, including semantic shape dimensions,
are outside this typed projection. Its `TokenExport.Validate` permits detached,
dependency-closed subsets and caller-supplied asset metadata. Selected modes
must expose the same colour/font identities. Shared colour references resolve
in each mode, and transition timing must be present in the selected values,
not merely known to a source owner. Asset-only or scale-only selections need
no mode, layout or assertion that fallback fonts are installed.

[DesignExport.WithTokens](ui/export/export_source.go) attaches that selection to an
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

[TokenExport.DTCG](ui/export/export_dtcg.go) projects one explicit mode into the
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
classes and rules into a stylesheet value. The result is four cascade layers —
`tokens`, `base`, `components`, `client` — and the `@layer` order statement
Compose emits is what says so: the kernel's ranking is the declared layer order,
not file position. A layer ranks before specificity, so which layer a rule goes
in is decided by what it must still win: the preflight sits in `base` where a
class outspecifies it, and a rule about one of the kernel's own components sits
in `components` beside the classes on that component, where its own selector
decides the tie. A consumer's class lists compile into the components layer
beside the kernel's (one rule per shared utility), and its hand-written rules
are placed in the client layer; a later layer wins normal declarations, so that
last layer is the strongest of the four, and what protects a kernel component
from it is the refusal below and not the ranking. `!important` reverses that
order, and the reversal is the point of the one important declaration the kernel
authors: the `prefers-reduced-motion` fallback sits in `base`, so it outranks a
consumer's `!important` animation in `client` and unlayered alike — an
accessibility floor a page cannot shout over. Compose refuses a consumer rule
that names, in whichever spelling a browser resolves to it, a kernel-rendered
attribute, one of the classes the kernel's own markup carries, the
root element, a `--pk-` property or a raw colour — its hex form or any
functional notation a browser computes a colour from (`rgb()`/`hsl()`,
`lab()`/`lch()`, `oklab()`/`oklch()`, `hwb()`, `color()`, `color-mix()`), a named
colour being a word a review catches rather than a pattern a gate can tell from a
keyword, and over the value a browser computes rather than every byte of the
value's text, with the argument of a `url()` reference and the contents of a
quoted string stepped over because neither computes a colour — in a rule or a
keyframe stop, and refuses it by the text it emits
rather than the one it was handed: a brace, a
comment start, a `--pk-` name after a semicolon or an at-keyword ahead of a brace
Compose supplies would move the block it opened, and a rule outside every layer
outranks the order statement; and the sequence that closes a `<style>` element,
which a composed sheet is the content of on the gallery preview, would leave the
sheet itself and reach markup. That refusal is by name, not by reachability: a
rule beside the consumer's own class is refused too, because the kernel's
attribute and class vocabularies are namespaces of its own markup and whether a
selector could reach a
kernel element is a question only a browser answers; a consumer renames its own
hook. Those refusals are the contract of the sheet a page links, and `Compose` is
where they sit: `ui/export`, the tool that renders a design proposal into a
snapshot, composes the same four layers with `ui.ComposeDesign`, which places
every rule identically and refuses none, because a capture exists to measure what
a browser computes for a sheet a mount would refuse — an authored colour with no
token yet, a margin on a component someone is proposing. The one style a page writes outside `Compose` is a value no Go sheet can
carry: [modules/web](modules/web/) pins its tenant's accent in an unlayered
inline declaration, guarded to `#rrggbb`, and unlayered is what lets a tenant
palette outrank every layer. The vocabulary is `components.Hooks` for the
components' markup and `renderedHooks` for the shell, the generated screens and
the gallery, and the classes, which `kernelClasses` computes from
`components.ClassLists()` and `composedClasses` widens per sheet to the lists it
resolves; a test refuses either vocabulary a name the kernel renders that it
omits. A bare type selector names neither
vocabulary, so `dialog { display: block }` reaches the modal from the strongest
layer and is the limit of the read, stated rather than closed. Deleting a
component should not leave an independently maintained stylesheet behind. The candidates
this shape was chosen from, each on the four questions the brief asked, are
[ADR 0018](docs/adr/0018-cascade-layers-decide-precedence.md).

[css.Sheet](ui/css/css.go) retains ordinary rule contribution order; only
adjacent equal selectors share a block. Repeated declarations remain ordered
so the browser can apply priorities, fallbacks and shorthand semantics. A later
consumer override must not be merged into an earlier rule ahead of utilities,
nor carry unrelated earlier declarations forward. Shared utility deduplication
belongs to the style owner, before emission. Layer order statements render
ahead of everything they order; the remaining at-rule group still renders
after ordinary rules; this is not an arbitrary CSS parser or minifier.

[Gallery](ui/components/examples/gallery.go) captures the existing constructor calls with
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

[export.Export](ui/export/export.go) projects those examples with their palette, glyphs
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

The [layout contract check](ui/export/export_layout.go) is a pure, all-occurrence
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

[export.ProjectProps](ui/export/proposal.go) and [export.ProjectReplacement](ui/export/replacement.go)
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

[RequestNoticeExamples](ui/document/document.go) captures the recovery content that
`document.Document` already serves, retaining its Stack, Alert and Link contracts;
`page.RequestNoticeExamples` applies the kernel's local-path rule to the sign-in link first.
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

[ui/document](ui/document/) models a document as shared `Chrome`, a plain
`Request`, a handler's `View` and `Document`, which puts them around the body a
`Frame` arranged; it reads no context and links no database. [ui/page](ui/page/)
is the router adapter: its `Request` carries the typed tenant and principal a
frame asks the Authorizer about, `page.Serve` reads it off the request, and its
`Chrome`, `View`, `Render` and notices are the document's under the names shells
already use. [ui/resource](ui/resource/) renders list, detail and form screens
from an entity schema and rows the same way; [ui/screens](ui/screens/) adapts an
`httpx.Resource` to it, mounts the screens its mounted routes answer and describes the
resource catalog at `/api/v1/app/resources`. Value words — how a boolean, an enum or an
instant reads — are [kit/entity/display](kit/entity/display/display.go)'s, with
`kit/rest` delegating. The admin module and downstream storefronts call these
packages rather than maintaining separate document or stylesheet machinery.

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

Account pages compose shared Form, Input, Button and Alert components. The existing
`session.js` accepts `data-login-form` or `data-auth-form="register|forgot|reset"`;
forms supply an API action, local `data-next`, and hidden `data-auth-error` (alert)
and `data-auth-message` (status) feedback. Registration sends email/displayName
for emailed password setup; it does not replace password-first verification.
Reset forms use a password named `new` and `page.View.Sensitive` for response
`no-store` and `no-referrer`; the controller removes the query token from browser
history and retains it only in memory. Incoming URL log redaction is separate.
Failed writes retain input without replay. Shared sign-out feedback remains
visible outside account menus; branding and page composition belong downstream.

## Keep delivery boundaries explicit

A downstream application pins the public module and adds its own composition.
Private catalog capabilities depend on the public contracts; the public module
never imports a private repository. Client configuration selects capabilities,
assets and branding. Client-specific Go belongs in a module, not in a
configuration directory.

The reference binary supports `--role web|worker|all`. Those roles do not make
every deployment topology safe: shared storage, schema compatibility, migration
ownership and provider behavior need environment-specific verification.

`Run` takes the address it serves on, so one process holds one composition per
address. [kit/app](kit/app/lifecycle.go) also exports `Start`: the same migration,
connection and boot gates, handing back a `Runtime` — its `Handler`, whose surface
is the whole API for `web` and `all` and the two probes for `worker`, its `Work`
half and its release — for an application that owns a listener of its own. Which
host reaches which composition stays the application's decision above each
handler: the kernel gains no registry, host list or second configuration namespace
from the seam, and each handler carries its own routes, asset prefixes and theme
because it is a separate router. That decision is therefore the caller's to make and
to make correctly: a started composition answers a host it was not composed for, and
only a route that must resolve a tenant refuses such a request — a public route and
the static tree answer with that composition's own page and own stylesheet. The
seam separates routers, not hosts.

[kit/httpx](kit/httpx/) sets response security headers and a request-nonce
content security policy. Inline style attributes remain an explicit allowance.
A page that instantiates WebAssembly declares it for its own response
(`httpx.AllowWASM`, or `document.View.RunsWASM` through the page adapter), which
adds `'wasm-unsafe-eval'` to that response's `script-src` and leaves every other
document's policy byte-identical.
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
[The verification workflow](.gitea/workflows/ci.yml) is the one that runs, in the
tiers decision 0088 rules: a pull request runs `make check-push` — the contracts
and the behaviour of the packages its diff reaches, and nothing that opens a stack
— and the push to main that merges it runs `make check-rehearse`, `make check` and
`make check-race`, then `govulncheck`, the native editor and browser checks,
`make e2e`, and the budget ratchet last. [`tests/inventory.json`](tests/inventory.json)
is the table that says which case belongs to which tier, and
`make check-test-inventory` refuses one the tree no longer answers. It is Gitea's because GitHub
Actions is disabled for this repository; [the retained GitHub
workflows](.github/workflows/ci.yml) copy its steps onto a runner destroyed with the job and do not run
merely because their files exist, exactly as [RELEASE.md](RELEASE.md) says of the
release workflow beside them. An absent GitHub check establishes nothing. A
publisher for the image, SBOM and release notes is not yet approved, so no tag
publishes anything until [RELEASE.md](RELEASE.md#publish-an-approved-version)
agrees one with the owner. The weekly
[public-consumption workflow](.gitea/workflows/public-consumption.yml) runs the
published-module and exported-API checks that `make check` deliberately does not,
the second against the v1.1.0 contract with no accepted-break baseline, so a new
exported break fails the step instead of joining a pile nobody reads.

A type check proves types, a test proves its exercised cases, and a product
journey proves its observed outcome. None alone proves production readiness,
design-file fidelity or reuse across different products. Report those claims
only with evidence at the same scope.
