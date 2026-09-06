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
[kit/app](kit/app/app.go) defaults to an in-memory transport for the combined
`all` role and JetStream for separate `web` and `worker` roles, unless the
application supplies a transport. An all-in-one local run therefore does not
prove broker delivery or durability across process restarts.

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
light and dark palettes. Components name semantic roles, roles resolve to
tokens, and a palette supplies the token values.

[ui/icon](ui/icon/) owns icons. [ui/components](ui/components/) provides typed
Go functions returning HTML and declares the classes those functions can emit.
[ui/style](ui/style/) resolves the declarations to CSS.
[ui.Compose](ui/ui.go) combines the shared declarations and a consumer's own
classes and rules into a stylesheet value. Deleting a component should not
leave an independently maintained stylesheet behind.

[Gallery](ui/components/gallery.go) captures the existing constructor calls as
flat examples with stable identities, typed properties and named Go slots.
The gallery still renders through those constructors. Property edits and
supported `Node` or `[]Node` slot replacements produce another bound example;
slot nodes are trusted Go capabilities, not user-supplied markup. Callbacks
and compound slot data are described but are not portable replacement inputs.

[ui.Export](ui/export.go) projects those examples with their palette, glyphs
and stylesheet into a content-addressed snapshot. Products supply their own
bound examples and reuse that boundary. An OpenPencil adapter must translate
the snapshot and separately prove native editing, sizing and save/reopen
behavior; the snapshot itself is neither another registry nor a JSON runtime
engine for constructing pages. Example identities are strings assigned in
gallery.go; the `pk-ui.component.` prefix is a namespace, not a reference to
another repository.
The export CLI can select an existing example and apply `Example.WithProps`
before calling `ui.Export`. This supplies source-owned rendered variations for
adapter comparison without reconstructing component behavior in another language.

[tools/designexport/openpencil](tools/designexport/openpencil/) owns native
adapter tooling, not another component catalog. Its version- and source-checked
SDK corrections operate on build/process inputs without modifying an installed
editor. Node conformance is a separate CI and tagged-tree gate; the correction
must also be integrated and verified in a browser build before editor release.
The native foundation generator reads the current Go export and produces native
token variables and linked icon components, with provenance on ordinary frames
and masters. It does not yet translate typed component examples, page composition
or product flows. See its guide for the supported SVG and token boundary.
Browser observations reuse the exported HTML and CSS rather than reimplementing
Go components. Source-owned text comments identify exact property regions without
adding layout elements. Observations and supplied-font checks are converter inputs,
not proof of native component editing or slot replacement. Experimental native
construction now binds one observed text region to its source string property;
its guide distinguishes tested geometry and persistence from the unfinished
component library and interactive editor.
Native tooling and tests have their own reviewed source budgets, separate from
the application's browser controllers.

[ui/page](ui/page/) models a document as shared `Chrome`, a request value,
a handler's `View` and a composing `Frame`. `page.Serve` adapts that
composition to the router. [ui/screens](ui/screens/) renders resource screens
and describes the resource catalog at `/api/v1/admin/resources`.
The admin module and downstream storefronts call these packages rather than
maintaining separate document or stylesheet machinery.

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
