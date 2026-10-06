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
| Entity and command fields | [`entity.Fields`/`FieldsOf`](kit/entity/schema.go) → [CRUD aliases](kit/crud/schema.go) → [`rest.Spec`/`Command`](kit/rest/rest.go) → authorized [`httpx.Resource`](kit/httpx/schemas.go). |
| Web forms and pages | [`forms`](ui/forms/forms.go) composes shared [components](ui/components/); [`resource`](ui/resource/resource.go) renders screens from a schema and rows, [`screens`](ui/screens/render.go) adapts authorized resources to it, and [`page.Serve`](ui/page/serve.go) supplies the caller's shell and request context around a [`document`](ui/document/document.go). A refusal the kernel makes before a handler exists — the cross-site guard, a panic — is the same problem value, shaped by whoever asked ([ADR 0015](docs/adr/0015-a-refusal-has-one-value-and-two-shapes.md)): `httpx.Options.Fault`, and the application supplies `page.FaultHandler(shell)` so a browser gets a page and a client gets the JSON. |
| Native discovery | [`screens.Describe`](ui/screens/catalog.go) exposes `/api/v1/app/resources`, stamped with `catalogVersion`; the native consumer owns its renderer. |
| Rich text field | A string with `ui:"widget:richtext"` enters [`richtext.Prepare`](kit/richtext/render.go) through `rest.Spec` inside the tenant transaction; the same package renders sanitized HTML for [`components.Prose`](ui/components/prose.go) in generated details and public content. [`richtext.Files`](kit/richtext/render.go) resolves images per tenant and audience, with [`modules/file.RichTextFiles`](modules/file/richtext.go) wired at composition. The schema publishes `contentMediaType: text/markdown`, and the no-JavaScript form uses a textarea with formatting help. |
| Component properties | [`Example.Describe`](ui/components/examples/example.go) derives Props JSON Schema, named slots and observed HTML from actual Go constructor inputs. |
| Design consumers | [`export.Export`](ui/export/export.go) and [`ProjectProps`](ui/export/proposal.go) produce snapshots and proposals; [source persistence](ui/source/source.go) has its own explicit API. |

Run `go test ./ui/screens -run 'ExampleFormExample|TestGeneratedForm'` to exercise
the [form-to-export example](ui/screens/design_test.go) and its composition checks.
This local check does not exercise a browser, native editor or business write.
Entity fields are flat scalars/lists; nested answer maps need an adapter. Component
Props describe presentation, while entity and answer contracts own data validity.
A2UI/MCP adapters need separate protocol wiring; source export does not supply it.

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

[apps/platformkit/modules.go](apps/platformkit/modules.go) is an ordered list
of module constructors. Each constructor accepts a typed `Deps` struct and
returns a manifest. There is no runtime discovery step. The compiler checks
dependency types; composition tests check required values and selected modules.

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
[modules/task](modules/task/) is the reference example. The manifest's
subscriptions, jobs and routes are typed by the kernel packages that run them;
[ADR 0013](docs/adr/0013-manifests-name-kernel-types.md) records why
`kit/module` is not a declaration-only leaf and what would move first if a
database-free reader of manifests appeared.

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
composition (`Theme: design.Default()`, `apps/platformkit/modules.go:235`), one
stylesheet serves every tenant, and no theme reads a row: isolation lives in the
database, and a stylesheet adds none of it.

**How it is traced and audited.** Nothing. `Pair` is comparable, so `ui` memoises
one stylesheet per pair rather than recomposing per request
(`design/design.go:165-166`); no span opens and no event publishes.

**How an app extends it.** Hand `ui.Compose` (`ui/ui.go:439`) a `Pair` of its
own, plus a `ui.Extra` (`ui/ui.go:102`) for rules. The reference product's own
line is `apps/platformkit/modules.go:285`: `design.Default()` is where a client's
colours go, and the only line that changes when they do.

### CSS and JavaScript

**Where it lives.** `ui/css` is the stylesheet's IR — `Sheet`
(`ui/css/css.go:130`), `AddRule` (`ui/css/css.go:142`) — and `ui/style` compiles
tokens into it (`ThemeVars`, `ui/style/theme.go:24`). Behaviour is controllers
served as static files beside the API (`ui/ui.go:2`), mounted by `ui.Assets`
(`ui/ui.go:529`) in `modules/web/internal/mount.go:78` and
`modules/admin/internal/mount.go:146`.

**Builds on.** `maragu.dev/gomponents` v1.3.0 (MIT) for the DOM the controllers
enhance, htmx 2.0.8 (0BSD) and the phosphor icon paths (MIT), both named in
`NOTICE`, and an IR derived from `github.com/septagon-oss/styleengine`,
Apache-2.0 (`ui/css/css.go:11`). Precedence is decided by cascade layers, and
only the client layer is a consumer's (`docs/adr/0018-cascade-layers-decide-precedence.md:1`).

**How the tenant crosses it.** It does not. What is enforced here is instead
mechanical: `scripts/check_ui_layers.sh` (`scripts/check_ui_layers.sh:13-24`)
refuses a raw utility class and a class no rule in its own layer styles, and runs
as `check-ui` (`Makefile:171`). `css.Literal` (`ui/css/css.go:46`) is kernel-only
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

**Where it lives.** `kit/tenancy` owns the port — `Policy`
(`kit/tenancy/policy.go:14`) deciding over a `PolicyResource` (`:36`) and a
`PolicyRequest` (`:43`) — and `RequirePolicy` (`:90`) is the boundary a service
calls. The adapter sits beside it: `kit/tenancy/providers/opa` builds a policy
from Rego source (`New`, `kit/tenancy/providers/opa/opa.go:45`) and answers
(`Decide`, `:74`).

**Builds on.** Open Policy Agent embedded as a library,
`github.com/open-policy-agent/opa` v1.21.0 (Apache-2.0): a composition writes its
rules in Rego and they run in process, with no policy server to deploy, reach or
keep in step (`kit/tenancy/providers/opa/opa.go:3-5`). Input is
`input.tenant.id`, `input.actor.kind`, `input.resource.*` (`:13`).
`kit/tenancy/providers/topaz` implements the same port over
`github.com/aserto-dev/go-authorizer` v0.24.1 (Apache-2.0) for a deployment whose
relations are served elsewhere.

**How the tenant crosses it.** Twice, both load-bearing:
`PolicyRequest.Validate` refuses a request whose resource carries another
tenant's id as an invalid request rather than a denial
(`kit/tenancy/policy.go:65`), and row-level security answers afterwards anyway —
a policy refines the database's constraint and never replaces it
(`docs/adr/0003-tenancy-by-postgres.md:1`).

**How it is traced and audited.** `kit/tenancy` starts no span of its own, so a
decision is seen inside the operation span the transport opened
(`kit/httpx/traced.go:86`) and in the refusal counter (`:140`, classed by
`RefusalClass`, `kit/telemetry/telemetry.go:212`), and reaches the trail through
the owning module's event. Each decision carries `sha256:` of the policy source
(`kit/tenancy/providers/opa/opa.go:19-20`), so the rules that answered are
nameable afterwards.

**How an app extends it.** It implements `tenancy.Policy`, and composition names
which one: `var taskPolicy = opa.MustNew(…)` at `apps/platformkit/modules.go:89`,
handed to the task module at `:222`, with the rules in the application's own
`apps/platformkit/policy/task.rego:1`. A denial (`ErrPolicyDenied`,
`kit/tenancy/policy.go:61`) belongs to the caller; a nil provider, a failing one,
or an answer that is not `{"allow": bool}` is an outage and never a permissive
default (`ErrPolicyUnavailable`, `:62`, refused by `RequirePolicy`).

### Authentication: sessions, TOTP, and WebAuthn

**Where it lives.** `modules/auth`: `Session` and its table
(`modules/auth/contracts/auth.go:105`, `:127`), and `Factors` as the seam for
anything beyond a password (`modules/auth/contracts/factors.go:119`) over a
`Factor` that carries its `Kind` (`:91`), which today is `totp` alone. The
browser's half is `kit/httpx/cookies.go` — `SessionCookie`
(`kit/httpx/cookies.go:21`), `CookieName` (`:42`), `SessionCookieOf` (`:58`).

**Builds on.** RFC 6238 in one file over `crypto/hmac`
(`modules/auth/internal/totp.go:23`): RFC 4226's 160-bit seed (`:41`), the digits,
step and skew fixed in the contracts (`modules/auth/contracts/factors.go:58-65`),
RFC 4648 base32 without padding (`modules/auth/internal/totp.go:45-46`), a
constant-time comparison over a window walked in a fixed order (`:114`), and the
`otpauth:` URI an app scans (`:133`). The file says why there is no library: a
wrapper around those thirty lines would move the review rather than remove it
(`modules/auth/internal/totp.go:28-29`). The
dependencies are `golang.org/x/crypto` (BSD-3-Clause), whose `nacl/secretbox`
seals the stored secret, and `github.com/coreos/go-oidc/v3` (Apache-2.0) for OIDC.

**How the tenant crosses it.** `sessions.tenant_id` under row-level security
(`modules/auth/migrations/000008_auth.up.sql:15`, `:41-48`) and the same for
`totp_factors` and `recovery_codes`
(`modules/auth/migrations/000031_auth_factors.up.sql:22`, `:47`, `:64`). The
secret column is sealed rather than hashed, because verification needs the value
and a backup must not carry a spendable factor (`:24-31`), and only the
deployment's key opens it — `svc.EnableFactors([]byte(deps.FactorKey))`,
`modules/auth/module.go:252`. Without a key the factor doors refuse
(`Deps.FactorKey`, `modules/auth/module.go:119`).

**How it is traced and audited.** Enrolment and withdrawal are events —
`auth.factor_enrolled` and `auth.factor_withdrawn`
(`modules/auth/contracts/events.go:161`, `:166`), with `auth.session_revoked`
beside them — and the trail holds them because the audit module subscribes to
every declared name (`modules/audit/module.go:93`). No span of its own: the
sign-in request is the router's.

**How an app extends it.** `Factor.Kind` is the opening, and the module's own
list of what is deliberately not here names both the shape and the cost of the
next kind — WebAuthn as `github.com/go-webauthn/webauthn` plus its CBOR/COSE
tree, a new dependency priced in its own budget commit, and a `webauthn.Config`
built per request from the resolved host (`modules/auth/README.md:188-192`).
WebAuthn is planned for T-0229, not on main: no `go.mod` names it and no Go
file here implements it, so today the only second factor is TOTP. The refusals a person
hits are correctable — a step replayed after it was spent
(`modules/auth/internal/totp_step_spend_refuses_a_replay_of_the_same_step_test.go`),
and a second factor offered without its first
(`modules/auth/internal/second_factor_requires_its_first_half_test.go`).

### Locales: kit/locale

**Where it lives.** `kit/locale` — `Messages` (`kit/locale/locale.go:9`),
`Formatter` (`:16`), `Locale` (`:21`), `SelectLocale` (`:29`) — with one
adapter, `kit/locale/providers/xtext`: `Load` builds a catalogue from ordered
sources (`kit/locale/providers/xtext/load.go:57`), `Catalog` and `Source`
describe what goes in (`:131`, `:138`), and `FromCatalog` hands back a
`locale.Messages` (`kit/locale/providers/xtext/catalog.go:16`).

**Builds on.** `golang.org/x/text` v0.42.0 (BSD-3-Clause) for language matching
and message catalogues (`kit/locale/providers/xtext/catalog.go:7-9`) — BCP 47
matching and CLDR plural rules. Nothing here is a service.

**How the tenant crosses it.** `tenancy.Languages` (`kit/tenancy/tenancy.go:53`)
hangs off the resolved tenant (`:44`) and off the resolved host
(`kit/httpx/tenant.go:49`, `:53`), and each request negotiates inside what that
tenant declared: `page.Serve` selects from `TenantPreferences`
(`ui/page/serve.go:101`, `ui/page/locale.go:61`). Why a refusal may not answer in
an undeclared language is the comment at `kit/httpx/tenant.go:280-283` — a page
one of them renders would negotiate from the caller's `Accept-Language` alone and
answer in a language this tenant refused. The declaration is a row
(`migrations/000029_tenant_locale.up.sql:1`).

**How it is traced and audited.** Nothing: `kit/locale` imports no
`kit/telemetry`, and selecting a locale writes no span and no audit row.
`e2e/localization.spec.ts` is what proves the promise held in a browser.

**How an app extends it.** Append an `xtext.Source` to the ordered list
`catalogues()` builds (`apps/platformkit/catalog.go:40`); the order *is* the merge
rule, and the comment above that function says why each source sits where it does
(`apps/platformkit/catalog.go:22-38`). Owner modules ship their own
`messages/<tag>.json`; `modules/notification` deliberately ships none, because its
words belong to the module that raised the notice. A request for a language its
tenant never declared is refused, and the fix is to declare it —
`scripts/e2e.sh:119-125` shows bootstrap's `--language` doing exactly that.

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

**How it is traced and audited.** Applying an accepted proposal goes through the
owning module's own command, so the audit row is that module's event and never a
change-module event; the contracts say so where `Subject` is declared
(`modules/change/contracts/change.go:254-266`). No span of its own.

**How an app extends it.** Implement `Subject` and bind it: `changeSubjects`
(`apps/platformkit/change.go:188`) and the one line that calls it
(`apps/platformkit/modules.go:279`) are the whole of what the reference product
adds. A refusal carries the owner module's answer in `Refusal`
(`modules/change/contracts/change.go:284`), which is why this module needs no
vocabulary of its own.

### Events: CloudEvents, the outbox, JetStream

**Where it lives.** `kit/events` — `Publish` (`kit/events/events.go:61`),
`PublishFor` (`:80`) for a system transaction that has to name its tenant,
`Consume` (`:199`), and the claim and dead-letter paths (`:265`, `:286`). The
wire format is its own file, `kit/events/transport/cloudevents.go`.

**Builds on.** CloudEvents 1.0 in structured content mode, hand-built: the
envelope (`kit/events/transport/cloudevents.go:41`) is what crosses a broker, so
a bridge, an event router or an AsyncAPI document can read a PlatformKit event
without importing this package (`:3-7`). `SpecVersion` is `"1.0"` and nothing
else is accepted (`:27`). The broker is NATS JetStream,
`github.com/nats-io/nats.go` v1.54.0 (Apache-2.0), from
`kit/events/providers/nats` (`Connect`, `kit/events/providers/nats/options.go:14`)
against the `nats:2-alpine` container (`compose.yaml:30`).

**How the tenant crosses it.** Three ways, all load-bearing: `Publish` takes a
`db.Tx[db.Tenant]`, so no call publishes without a tenant transaction
(`kit/events/events.go:61`); the `tenantid` extension is required here, because an
event with no tenant has no transaction to deliver it in
(`kit/events/transport/cloudevents.go:50-54`); and the outbox rows are filtered by
row-level security (`migrations/000002_outbox.up.sql:15`, `:37-38`).

**How it is traced and audited.** W3C trace context travels verbatim as `traceparent`
and `tracestate` (`kit/events/transport/cloudevents.go:56-62`), plus a PlatformKit-
owned `baggage` carrying the publisher's request id, because the specification fixes
those two members and says nothing about correlation (`:63-70`; the column is
`migrations/000041_outbox_baggage.up.sql:42`). Spans: the relay's batch
(`kit/events/relay.go:261`), `<event> publish` and `<event> deliver`
(`kit/events/trace.go:143`, `:178`). The trail *is* this stream: `SubscribeAll`
(`modules/audit/module.go:93`) turns every declared event into a row.

**How an app extends it.** Declare in the manifest — `Declared` and
`Subscriptions` (`kit/module/module.go:71`, `:75`) — and write one transport line
at composition: `app.Transports{Memory: memory.New, JetStream: eventnats.Connect}`
(`apps/platformkit/modules.go:312`), in the function the application calls the one
place it names an event provider (`:311`). `kit/app` selects by name and imports
no provider (`kit/app/app.go:186-192`). An unknown `specversion` is refused rather
than guessed at, and an event with no tenant cannot be formed; a claimed event
that is already handled is a no-op, so delivery is at-least-once and an external
effect still needs its provider's idempotency contract.

### Notifications

**Where it lives.** `modules/notification`: `Notification`
(`modules/notification/contracts/notification.go:29`), the `Notice` a refusal
raises (`:70`), `RecipientLookup` (`:84`) and `HostLookup` (`:101`) as what it
needs from other modules, `Mailer` (`:127`) as what it does not, and `Service`
(`:134`). It ships no `rest.Spec`, and the manifest says why: a Spec's list route
is the whole tenant, and these rows are one person's (`modules/notification/module.go:5`).

**Builds on.** `net/smtp`-shaped SMTP (`NewSMTP`,
`modules/notification/internal/smtp.go:78`) and one Go template. No mail library,
and no message catalogue of its own.

**How the tenant crosses it.** `notifications.tenant_id` under `ENABLE` and
`FORCE` with the tenant-match policy
(`modules/notification/migrations/000011_notification.up.sql:13`, `:36-41`), plus
the delivery ledger in `000027_notification_deliveries`. An address and a host
arrive only through the two ports the application implements over the user and
tenant modules, so this module never names either
(`modules/notification/module.go:31-45`).

**How it is traced and audited.** Its three events
(`modules/notification/contracts/events.go:22-24`) reach the trail through
`SubscribeAll`; no span of its own.

**How an app extends it.** Implement `Mailer`. Composition makes the one choice:
SMTP when configured, the mailbox when not (`apps/platformkit/modules.go:342-344`),
and the mailbox is no stub — "it keeps every message and logs each one"
(`:339-340`) — so a deployment without mail records everything and says what it
would have sent (`apps/platformkit/main.go:82`). A refusal a person must see
raises a `Notice` (`apps/platformkit/modules.go:504`). A nil lookup writes every
row and sends no mail: the notice is the record, the mail is a copy.

### Object storage: modules/file over S3

**Where it lives.** `modules/file`: `Scope` (`modules/file/contracts/scope.go:39`)
with `ScopeOf` (`:42`), `ScopeOfTx` (`:53`) and `Scope.ObjectName` (`:92`) as the
seam between a tenant and a name; `Deps` and `S3Config`
(`modules/file/module.go:71`, `:56`) as the wiring; and two adapters,
`modules/file/internal/s3.go` (`NewS3`, `:68`) and `modules/file/internal/local.go`.

**Builds on.** `github.com/minio/minio-go/v7` v7.3.0 (Apache-2.0) over the S3
API, which the module names as several stores — AWS S3, Garage, SeaweedFS, Ceph
RGW (`modules/file/module.go:43`); locally it is tested against the SeaweedFS
image pinned by digest (`compose.yaml:100`). The client library is the dependency;
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
the reference app hands it to content (`apps/platformkit/modules.go:212`,
`modules/content/module.go:71`). Over quota is correctable — free space, or ask
for quota (`modules/file/module.go:83-87`); a store that cannot prove erasure is
not, at that adapter: S3 leaves `verified_at` NULL and leans on a bucket lifecycle
rule (`:51-52`).

### Cache: kit/cache over Valkey

**Where it lives.** `kit/cache`: `Scope` (`kit/cache/cache.go:98`), built only by
`Of` (`:112`) or `Shared` (`:128`), `Key` (`:154`), `Cache` (`:186`) and `Value`
(`:240`); the adapter is `kit/cache/providers/valkey` (`New`
`kit/cache/providers/valkey/valkey.go:67`, `Connect` `:78`). What may be cached at
all is the package comment: values a replica may recompute, and never sessions,
permission grants or entitlements (`kit/cache/cache.go:22-27`).

**Builds on.** `github.com/redis/go-redis/v9` v9.22.0 (BSD-2-Clause) against
`valkey/valkey:8-alpine` run with `--maxmemory-policy noeviction`
(`compose.yaml:82-83`). The kernel imports neither a Valkey server nor a
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
(`kit/cache/cachetest/conformance.go:28-30`) so a key that lets one customer reach
another is reproducible from the failure message alone.

**How an app extends it.** Add a field to `app.Caches` (`kit/app/app.go:155`,
selected at `:510`); the reference composition names `valkey.Connect` and nothing
else (`apps/platformkit/modules.go:320`, in `caches()` at `:319`). A selected
adapter with no constructor is refused before anything opens
(`kit/app/app.go:167`). `noeviction` is a requirement and not politeness: the
counter a `Move` raises carries no TTL, an evicted counter reads back as
generation 0, and what `Move` closed reopens (`compose.yaml:77-81`) — a deployment
fault, not a caller's.

### Metrics and traces: OpenTelemetry

**Where it lives.** `kit/telemetry` names the attributes and the instruments and
holds no provider: `Scope` (`kit/telemetry/telemetry.go:68`), `Tracer` (`:88`),
the attribute names (`:94-97`), `WithRequestID` and `RequestID` (`:136`, `:153`),
`SpanAttrs` (`:160`), `MetricAttrs` (`:188`), `RefusalClass` (`:212`), and
`Instruments` (`kit/telemetry/instruments.go:34`). `kit/trace` carries the
context — `Context` (`kit/trace/trace.go:59`), `Parent` (`:70`), `Parse` (`:100`) —
with the header names written once (`:27-31`). `kit/app` installs the one
`TracerProvider` and `MeterProvider` (`kit/app/telemetry.go:250`, `:262`).

**Builds on.** `go.opentelemetry.io/otel` v1.46.0 (Apache-2.0) and its OTLP/gRPC
exporters; W3C Trace Context, which `kit/trace` implements rather than wraps. The
collector is a separate program (`compose.yaml:64`).

**How the tenant crosses it.** Every span and number is stamped from the context:
`SpanAttrs` puts `pkit.tenant` — the slug a person recognises — and
`pkit.tenant.id` on a span, with `pkit.request.id` on spans only
(`kit/telemetry/telemetry.go:94-97`, `:160`), and `MetricAttrs` is the same list
without the request id, because a value unique per request makes one series per
request (`:188`, `:173-186`). The tenant is never a resource attribute: one
process serves many tenants, as `## Follow a request` says.

**How it is traced and audited.** This pillar *is* the trace: the transport span
(`kit/httpx/traced.go:50`), named after the operation id (`:86`), the transaction
span (`kit/db/tx.go:284`), publish and deliver (`kit/events/trace.go:143`, `:178`),
the relay batch (`kit/events/relay.go:261`), and the job run with one span per
tenant (`kit/jobs/jobs.go:192`, `:322`). The audit row stores `traceparent`
verbatim and indexes the trace out of it, as the audit section below explains.
The instruments are the three named at `kit/telemetry/instruments.go:61-69`,
with latency buckets that run to a minute because the work slow enough to matter
is slower than the SDK's defaults (`:22-25`). `kit/telemetry/README.md` holds
the measured figures; this page quotes none.

**How an app extends it.** A module never holds a provider. A deployment writes
`telemetry.otlp_endpoint` and `telemetry.sample_ratio`
(`kit/config/config.go:85-108`), whose zero value exports nothing
(`kit/config/config.go:39-41`), and any package names attributes through
`SpanAttrs`. An unreachable collector must not stop serving, so OpenTelemetry's
own error handler keeps serving (`kit/config/config.go:84`).

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
restated at `modules/audit/README.md:38`). No hash chain is on main.

**How an app extends it.** Nothing per app, and that is the point:
`SubscribeAll: true` (`modules/audit/module.go:89-93`) is expanded by the kernel
into one subscription per declared event after every manifest is read, so a module
added later is audited by having emitted an event and by nothing else. Retention is
a `Deps` field rather than a constant, because which retention a deployment owes is
the composition's decision (`modules/audit/module.go:50-61`).

### End-to-end and flows

**Where it lives.** `e2e/` — 27 specs, one Playwright config
(`e2e/playwright.config.ts:10`) and shared steps in `e2e/steps/content.ts` —
behind the `e2e` goal (`Makefile:112`) and `scripts/e2e.sh`.

**Builds on.** `@playwright/test` ^1.63.0 (Apache-2.0), Chromium only, one worker,
no retries: a gate, not a compatibility matrix
(`e2e/playwright.config.ts:6-8`). It is a devDependency that drives a browser, not
a dependency of the runtime.

**How the tenant crosses it.** Each run boots the application against a database
of its own, migrated from nothing, and creates one tenant and one administrator
with `platformkit bootstrap --tenant e2e --host localhost`
(`scripts/e2e.sh:123-125`); the browser then arrives at that tenant's host.
Playwright starts nothing, because the teardown has to happen whichever step
failed (`e2e/playwright.config.ts:4`, `scripts/e2e.sh:11-12`).

**How it is traced and audited.** The specs assert refusals and rendered state,
and that every table is scoped to its tenant is proved by `migrations/rls_test.go`,
which walks every table the kernel's and every reference module's migrations
create (`migrations/rls_test.go:45`). No span belongs to this pillar: it is the
pillar that checks the others.

**How an app extends it.** Add a spec, and share steps through
`e2e/steps/content.ts`. A spec that passes for the wrong reason is caught by
`retries: 0` and by the run's own database: a flaky spec is a bug report, not a
retry.

### Mobile

**Where it lives.** The contract is a document. `ui/screens` publishes the screen
and operation catalogue — `CatalogVersion` (`ui/screens/catalog.go:25`), `Catalog`
(`:31`), `Entry` with the operation ids an app calls (`:42`, `:77`) — at
`/api/v1/app/resources` (`kit/httpx/surfaces.go:77`); `kit/app` renders the event
half as AsyncAPI 3.0.0 (`kit/app/asyncapi.go:1`, `AsyncAPI` `:47`, `CoveredEvents`
`:146`), pinned by the golden files in `apps/platformkit/testdata/`.
`apps/platformkit/wire_compatibility_test.go` is the gate, with rules B1–B6 quoted
at `:13-21`, and `e2e/maestro/flows.json` is the device journey behind
`mobile-e2e` (`Makefile:120`) and `.gitea/workflows/mobile.yml:1`.

**Builds on.** OpenAPI 3, emitted by `github.com/danielgtaylor/huma/v2` v2.39.1
(MIT), and AsyncAPI 3.0.0 as a document this repository writes itself. Maestro
runs the journey as a separate program (`scripts/mobile_e2e.sh`), so it is not a
dependency. Screens are derived from schemas rather than authored
(`docs/adr/0007-screens-are-derived-from-schemas.md:1`).

**How the tenant crosses it.** The document is reached behind host resolution, on
the tenant's own address, so a shell never names a tenant
(`kit/httpx/surfaces.go:77`), and every operation inside it carries the tenant
through the request's transaction like any other call. `CoveredEvents`
(`kit/app/asyncapi.go:146`) counts the declared events the document names, which
is the claim that a shell sees no event nobody declared.

**How it is traced and audited.** Nothing new: the events a shell may see are the
events the events section traces and the audit section records, on the same
trace. The documents are produced at boot and their bytes are pinned, so a
renamed JSON tag fails `make check` rather than a shipped build.

**How an app extends it.** Nothing — a shell renders the document, and a
product's share is not to extend it but not to break it. Nothing overrides a
refusal, including `UPDATE_GOLDEN=1`; a planned breaking change ships as a new
address plus an alias row (`apps/platformkit/wire_compatibility_test.go:24-31`,
`httpx.API.Alias` and `module.Module.Moved`). B1–B6 are immutable to the change —
a removed operationId, a retyped schema member, a new required field, an enum that
strands a reader, a narrowed authorisation — and additive change is always allowed
(`:22-23`). `apps/platformkit/catalog_version_test.go` is the companion proof that
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
address. [kit/app](kit/app/app.go) also exports `Start`: the same migration,
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
[The verification workflow](.gitea/workflows/ci.yml) is the one that runs: `make
check`, then `make check-race`, `govulncheck`, the native editor and browser
checks, `make e2e`, and the budget ratchet last. It is Gitea's because GitHub
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
