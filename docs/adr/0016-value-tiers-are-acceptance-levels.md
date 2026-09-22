# 0016: A tier is the acceptance a product must survive, not a package it must contain

Status: accepted, 2026-09-19. An independent review checked the source and import claims against
the code and ran the package gate. It qualifies no application: a product reaches a tier by its own
evidence, and the per-product decisions below stay the product's. Claims are of two kinds — what
the code does today at the revision a consumer pins, and *target*, a direction or behaviour the
code does not have, or has no check for, today.

## Problem

The five bands named here say what a product must survive. Each was acquiring a second meaning:
"this product must survive multi-actor use" sliding into "so it needs a package of its own",
and thence into a directory layout per band. A tier also reads like a price, which it is not.
Separating the meanings is the decision; the package rules already belong to
[ADR 0002](0002-explicit-wiring.md), [ADR 0012](0012-independent-parts.md) and
[ADR 0013](0013-manifests-name-kernel-types.md).

## Decision: a tier is an acceptance floor

| Tier | Product outcome | Acceptance someone outside the team can observe |
| --- | --- | --- |
| **T1 Capture** | Replaces a manual record or submission | An authorized actor submits; the record survives reload and restart; the product's stated refusal is refused, with a reason and no partial write; an entitled party retrieves it; the data that must leave, leaves. |
| **T2 Coordinate** | Several people around one piece of work | Two actors with different grants complete one journey; each refusal is *explained* and, where the correction is legitimate, correctable; ownership and history say who did what. |
| **T3 Operate** | A recurring business operation | The operation repeats on schedule and recovers; concurrent commands on one row settle deterministically and a refused one changes nothing; a periodic report reproduces from stored state. |
| **T4 Connect** | Several systems or teams | A replayed or out-of-order effect does not double-apply; a conflict surfaces with both sides; an integration failure is visible and an operator has a bounded way to resolve it. |
| **T5 Assure** | A high-value decision with controlled evidence | A named accountable person accepts the behaviour; the inputs and policy version used are recoverable afterwards; restricted actions are restricted and each outcome traces to its inputs. |

- **Tiers are floors, not launches.** Reaching T4 or T5 does not re-launch T1–T3: the higher
  evidence *preserves the applicable lower-level correctness*, and no T4 or T5 result waives T1
  durability or access. A T4 acceptance run still shows that a submission persisted and that an
  unauthorized reader cannot read it.
- **Not every refusal is reversible.** T2 asks that a refusal be explained and correctable *where
  correction is valid*. A refusal that keeps something immutable, satisfies a legal hold or closes
  a security hole must persist by not being correctable; a case that "repairs" it tests the wrong
  thing. Say which class each refusal is in.
- **A tier is not a price.** Prices are owned elsewhere ([ADR 0008](0008-prices-are-the-operators.md),
  [`modules/billing`](../../modules/billing/contracts/billing.go)): a band and a plan change for
  different reasons.

## Decision: the layer names are not the rule; the dependency direction is

Listing "core / backend / frontend / adapters / integration" decides nothing, because names do not
stop an import. What binds is which package may depend on which. The table gives a **target**
direction per position and, in its own column, what the code and the gates show today. Read a row
as "this position may depend on these", not as a point on a line: the positions are not a chain,
and an adapter composing an adapter is a recorded decision — `ui/screens` imports `ui/page` in
`render.go` and `screens.go`, and [`scripts/check_packages.sh`](../../scripts/check_packages.sh)
lists that import in `ui/screens`'s permitted closure.

| Position | What it owns | Target: what it may depend on | Current evidence |
| --- | --- | --- | --- |
| **Pure core** | Values, field metadata, pure rules, selection and formatting contracts | The standard library, its own value types, and an explicitly chosen pure value library where a value needs one — `uuid` today, the same class as a text-normalization package — named in the gate's allowed closure for the package that uses it. The line is what a package imports, not whether it is external. Not SQL, HTTP, a cloud or broker SDK, configuration loading or `main`-like startup | Enforced for these: [`scripts/check_packages.sh`](../../scripts/check_packages.sh) runs `go list -deps` over its recorded parts and refuses `database/sql` and `net/http` outside the permitted modes; `kit/entity`, `kit/entity/display`, `kit/locale`, `kit/fault`, `kit/flags`, `kit/tenancy`, `modules/task/domain`, `design`, `ui/forms`, `ui/document`, `ui/resource` are held in the non-SQL, non-web modes, and four of them — `kit/fault`, `kit/locale`, `design` and `modules/task/domain` — to an empty closure, the standard library and themselves. [`modules/task/domain`](../../modules/task/domain/README.md) is the worked example; [`kit/fault`](../../kit/fault/README.md) is the empty one, and the reason it has to be: a package that refuses a write before any transaction exists cannot name the refusal through the adapter that classifies one. |
| **Backend** | Typed commands and queries over the domain, plus *narrow explicit effect ports*: the events a state change announces, and the contracts another capability needs (`events` transport and sink, file `Storage`, notification `Mailer`, `tenancy.Policy`, `flags.Evaluator`) | The core, and the ports just named. Reaching an SDK, a driver, a route declaration or a background goroutine is what the target refuses | [`kit/events/transport`](../../kit/events/transport/) is `Event`/`Transport`/`Sink` with only standard-library and UUID dependencies — the portable shape. [`rest.Spec`/`Command`](../../kit/rest/rest.go) declares routes, permissions, immutable fields and events in a **mixed package** that reaches `database/sql` and `net/http` in one import, so it is evidence of the content and of rule 1, not of the direction; [`modules/content`](../../modules/content/module.go) is one module of that shape. |
| **Frontend** | Detached component props and named slots, controller *contracts*, and declared assets | The core and the presentation values it owns — props, named slots, controller contracts, declared assets — with every service effect left to the caller: no route, no transaction, no field meaning | Component Props and named slots are derived from real constructor inputs by [`Example.Describe`](../../ui/components/examples/example_composition.go); a page's controllers are declared inputs, not globals — [`ui/document`](../../ui/document/document.go) carries `Scripts` as ordered file names and `Principal` so a controller learns who is asking, and `kit/app` cannot compose presentation ([ADR 0015](0015-a-refusal-has-one-value-and-two-shapes.md) makes the same argument); assets are declared with their digest and are explicitly "not another asset registry" ([`design.Asset`](../../design/assets.go)). |
| **Adapters** | The implementation of one port against one runtime: a database, a broker, a translation catalog, a policy engine, the HTTP and screen surfaces | The port it implements and the core — plus an adapter it composes, where that import is recorded. What is refused is *becoming the port's definition*, not contact with another adapter | [`ui/page`](../../ui/page/README.md) and [`ui/screens`](../../ui/screens/render.go) are the gated adapters through `kit/httpx`, the second composing the first under the recorded import named above; the provider packages under `kit/*/providers/` are each selected, never discovered. |
| **Integration** | Tenant transactions, route mounting, job and subscription wiring, provider selection, migrations of the running composition | The backend, frontend and adapter positions, and no domain rule | [`kit/httpx`](../../kit/httpx/httpx.go) supplies the request's tenant-scoped transaction; a [manifest](0013-manifests-name-kernel-types.md) names permissions, events, jobs and routes; [`kit/app`](../../kit/app) mounts them and selects the transport it was handed. |
| **Product composition** | Joining the above with concrete values, in an order somebody wrote down | The constructors of all of the above, and no rule that belongs to a capability | [`apps/platformkit/modules.go`](../../apps/platformkit/modules.go) is an ordered list of constructors taking typed `Deps`; [`scripts/check_imports.sh`](../../scripts/check_imports.sh) enforces that modules consume `contracts/` and only an application names constructors ([ADR 0002](0002-explicit-wiring.md)). |

Three rules follow:

1. **Only the portable parts carry a strict target; existing mixed packages stay mixed until a
   traced consumer shows the benefit.** Several packages here are today one import that serves
   several positions — `kit/rest` reaches the database and the HTTP surface together, and a
   module's `internal` package holds its decision and its SQL beside each other. Splitting either
   into a pure core and an adapter is an extraction under
   [ADR 0012](0012-independent-parts.md): trace the consumer, show the before and after code, keep
   one implementation. Naming a tier is not that trace.
2. **No package template.** A module stays `contracts/` + `internal/` + `module.go`. A tier, and
   each position above, is a *responsibility*, not a directory that must exist: a small library
   does not grow five packages to look well-layered.
3. **No new Go module, and no registry, to express a direction.** The direction is checked by the
   scripts above and by the ceilings in [`packages-budget.json`](../../packages-budget.json) and
   [`loc-budget.json`](../../loc-budget.json); separate Go modules require independently managed
   versions and consumers, which is a release decision
   ([ADR 0012](0012-independent-parts.md), [RELEASE](../../RELEASE.md)). The package script checks
   only the parts in its own `parts` list, under the mode assigned to each: the rest of the table is
   a review rule until a check reads it.

## T1 is not a public default

A T1 product either publishes — an anonymous read at a host that resolves to a tenant — or gates
its reads behind a permission, or does neither and is an internal tool. `httpx.Auth` has four
declared kinds — `Permission`, `OperatorPermission`, `Public`, `SignedIn`
([`kit/httpx/auth.go`](../../kit/httpx/auth.go)) — guarding the same route: a `rest.Spec` list
route takes its read permission, and a `rest.Singleton` may declare a public face, which has to name
the `Face` it serves so a public route cannot answer with the whole row. Which applies is the
product's decision; the tier asks that the choice be tested against the data actually held —
tenancy, authorization, persistence, the stated refusal, and retention or export *as applicable*.

## The common engineering decisions

A specification answers ten questions per product; the vocabulary has one owner each and the answer
is the product's. Where a row names a mechanism from one module, that is an **example of a shape**,
not a claim every module has it.

| Question | Existing today | Product decision, or a funded extraction | Owner |
| --- | --- | --- | --- |
| Identity and aliases | A `uuid` primary key plus a soft-delete and tenant column in [`entity.Base`](../../kit/entity/entity.go) — which carries no human-readable name. [`modules/user`](../../modules/user/contracts/user.go) additionally offers an **optional** per-tenant `handle`: unique in the tenant, renameable, not releasable, claimed by command. | A name for anything that is not a user. There is no general alias mechanism: an app that needs a code, a slug or a ticket number owns its grammar and its uniqueness, and keeps the uuid as the key. | [ADR 0014](0014-a-handle-is-an-alias.md) (users only) |
| Typed field ownership | `entity.Fields` → `crud.Fields` → `rest.Spec`/`Command` → `httpx.Resource`; enum and validate tags feed the API document and the derived form. | Units, semantic meaning, a versioned collection definition. Entity metadata describes compiled fields. | [Entity and presentation contracts](../../ARCHITECTURE.md#entity-and-presentation-contracts), [ADR 0012](0012-independent-parts.md#schema-and-presentation-ownership) |
| Commands vs CRUD | Five routes from a `Spec`; `rest.Command` where a state rule needs its own permission and event; `Spec.Immutable` refuses generic writes to command-owned fields. Content's draft → published → archived is one example. | A workflow or rules engine, and any guard hook: `CommandOptions` carries only `Auth` and `Collection`. | [`kit/rest`](../../kit/rest/rest.go) |
| Authorization on every surface | A permission per route, resource and command; the operator boundary; and the same declaration enforced on API routes, resource closures, discovery and generated screens. Object scope exists where a product writes it: `tenancy.Policy` decides one object (`PolicyResource.ID` plus trusted attributes) and [`modules/task`](../../modules/task/internal/policy.go) calls it per row. | **Target:** a declared, general per-row and per-field rule. Nothing in `rest.Spec` answers "this actor may see this column of this object", and field-level rights have no owner here, so today a product needing object scope supplies and owns its own `tenancy.Policy` check and says who answers. | [`kit/httpx/auth.go`](../../kit/httpx/auth.go), [`kit/tenancy/policy.go`](../../kit/tenancy/policy.go), [ADR 0003](0003-tenancy-by-postgres.md), [ADR 0008](0008-prices-are-the-operators.md), [ADR 0007](0007-screens-are-derived-from-schemas.md) |
| Transaction and concurrency | One transaction per request; `crud.GetForUpdate` row lock; a named advisory xact lock inside the owning transaction where a critical section spans rows ([`modules/file`](../../modules/file/internal/service.go), [`modules/auth`](../../modules/auth/internal/registration.go)); a refused mutation emits no event and returns no stale row. | **Target:** an optimistic revision. `entity.Base` has no version and write routes take no `If-Match`, so an idempotent command stops a double transition but **not** a lost update — an approval can land on a revision that moved after it was read. | [`kit/crud`](../../kit/crud/crud.go), [concurrency cases](../../kit/rest/concurrency_test.go), [ADR 0003](0003-tenancy-by-postgres.md) |
| Effects and idempotency | Transactional publication through the SQL outbox, tenant-scoped handling claims, terminal records, a retry ladder, at-least-once delivery; the event id is the deduplication key. | **Target:** durable handler idempotency. Each sink supplies its own; an external effect still needs provider-side idempotency; recreating an incompatible durable may replay. | [`kit/events`](../../kit/events/README.md), [ADR 0004](0004-events-are-the-job-queue.md) |
| Retention, export, delete | `Spec.SoftDelete` writes `deleted_at`, and **the read paths in `kit/crud`** — get, count, list, update, delete — filter it. Audit rows carry a composed `RetentionDays` sweep. | **Custom and raw reads are not covered by that filter.** A product that queries its own table directly, aggregates, joins or exports is responsible for the same predicate, and its export/erasure acceptance has to name which read paths it checked. **Target:** subject-data export and erasure have no owner here; a product holding personal data needs the decision, and a shared mechanism needs its own ADR. | [`kit/crud`](../../kit/crud/crud.go), [`modules/audit`](../../modules/audit/module.go), [ADR 0011](0011-migration-ownership.md) |
| Explicit providers | Typed `Deps` selected at composition; provider packages behind contracts owned by the capability that needs them; a package gate on transitive runtime imports. `kit/httpx` is HTTP *server* routing and guards — not an outbound client, not a provider boundary. | Any provider reached without a named `Deps` field, and any universal provider registry. Whether a product needs a provider at all is its own answer at every tier, including T5: assurance over local computation needs no integration. | [ADR 0002](0002-explicit-wiring.md), [Provider boundaries](../../ARCHITECTURE.md#provider-boundaries), [`scripts/check_packages.sh`](../../scripts/check_packages.sh) |
| Locale, money, time | Language selection and formatting contracts with an `x/text` provider; page localization; money as an `int64` minor unit with a `Currency` and explicit bounds where a module bills; UTC timestamps. | Translation administration; unit and currency meaning outside billing; rounding; effective-dating. | [`kit/locale`](../../kit/locale/README.md), [`ui/page`](../../ui/page/README.md), [`modules/billing`](../../modules/billing/contracts/billing.go) |
| Specialist validation | Nothing in `kit/`, `modules/`, `ui/` or `design/`: no certification, no expert registry, no specialist-correctness score. | See the next section: required where the domain rule asserts a fact only a specialist can supply, and answered by engineering review where it does not. | The product's owner |

## Specialist validation is per rule, not per app

Validation is named, dated evidence from a person accountable for a claim, attached to a revision
and to the cases that exercise it. It is owed **where the product's own rule asserts something only
a specialist can supply** — a clinical, legal, tax, safety or physical fact, a jurisdiction variant,
an eligibility judgement. A product whose rules are arithmetic, bookkeeping of its own records or
the shape of a form does not acquire an external sign-off by being classified into a tier: its owner
accepts it by reviewing the code and the cases, which is a complete answer for a first-party
engineering product. What is refused is the instrument, not the review — a feature that "certifies"
an output, or a badge on a row, is a claim a user interface cannot earn. Where real assurance is
needed the evidence lives beside the tests and the release note, and T5's observable acceptance is
that the version used can be recovered afterwards.

## Consequences

- *Tier* has one meaning in this repository. A document that means price says price; one that
  means composition names the packages and who imports them.
- The *Target* cells are visible as gaps: a declared per-row and per-field rule, an optimistic
  revision, subject-data export and erasure, durable handler idempotency. A product whose rule needs
  one implements it or funds an extraction under [ADR 0012](0012-independent-parts.md); recording
  the primitive as existing is not an option.
- Naming a tier authorizes no package, no module, no migration and no deployment. A change that
  wants a new module has to name the consumer whose cost fell.
- Two gate limitations, both open and not by design: `database/sql` is matched exactly, so a
  types-only `database/sql/driver` edge passes (`kit/entity` reaches it through `uuid` and the gate
  stays silent) while `net/http` is matched as a subtree; and a package absent from
  `check_packages.sh`'s `parts` list is not checked at all.
- Unresolved and deliberately left so: no tier is instrumented. A change that wants a
  machine-readable tier marker must first say which observable acceptance it records and which
  test answers for it.
