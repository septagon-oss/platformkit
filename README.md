# PlatformKit

PlatformKit is a Go foundation for composing multi-tenant SaaS applications.
It brings together a runtime, reference business modules, typed web components
and an operator interface generated from resource schemas. Applications choose
modules through explicit Go dependency structs and supply their own capabilities
where the product needs them.

Start with the application below. Read [Architecture](ARCHITECTURE.md) to locate
an implementation, [Contributing](CONTRIBUTING.md) to change it, and
[AGENTS.md](AGENTS.md) when working with a coding agent.

## Run the reference application

One command, with Go installed and nothing else:

```sh
go run github.com/septagon-oss/platformkit/apps/platformkit@latest start
```

`start` runs its own PostgreSQL 16 — downloaded once into your user cache,
its data under `./data` — creates the application role, migrates, bootstraps
the first tenant and administrator when there is none, and serves on `:8080`.
The generated administrator password is printed once; keep it private. A second
`start` in the same directory finds the tenant and serves it again.

Open [the local sign-in page](http://platformkit.localhost:8080/admin/login).
The request host selects the tenant, so use `platformkit.localhost`, not an
arbitrary host alias. The component gallery is at `/admin/_gallery`.
`--data` and `--addr` move the data directory and the listening address.

From a checkout, the development loop is the Compose stack and `run`:

```sh
make up     # PostgreSQL and NATS, waiting for both
make run    # the application on config.yaml, created from config.example.yaml when missing
```

`run` is what a deployment uses too: a `config.yaml` of its own and a database
of its own. Set `PLATFORMKIT_PG_PORT` and `PLATFORMKIT_NATS_PORT` when running
the Compose stack and the tests beside another one, and check
`docker compose ps` before starting a second stack. The example configuration
is for local development, not a production deployment.

## Compose a product

[modules/task](modules/task/) shows a capability's shape: `contracts/` owns
its public behavior and conformance tests, `internal/` implements it, and
`module.go` accepts dependencies and returns a manifest. The reference
application lists its constructors in
[apps/platformkit/modules.go](apps/platformkit/modules.go).

A module may depend on another module's `contracts/`, never its implementation
or constructor. Supply the dependency in the application's composition.
Compilation checks its type; composition and behavior tests check that it is
present and does what the consumer needs.

The reference modules cover tenants, users, authentication, audit,
notifications, billing, content, files, sites, tasks and administration. The
built-in billing provider records charges but does not move money. A real
payment processor is a separate implementation of the public contract.

A downstream application pins this Go module by version and composes it with
its own modules. This repository does not need access to private catalog or
client repositories to build.

## Build a screen

[design](design/) owns the theme values and typography.
[ui/icon](ui/icon/) owns icons, [ui/components](ui/components/) owns typed
components, and [ui/style](ui/style/) resolves their declared classes.
[ui.Compose](ui/ui.go) combines the shared stylesheet with a consumer's
declarations. [ui/page](ui/page/) represents documents and serves them through
one router adapter; [ui/screens](ui/screens/) renders resource-based screens.

Use the generated screens for record management. Compose a custom page for
a workflow that needs its own interaction, and test the resulting journey.
A client palette is a `design.Pair`; components continue to use semantic roles.
There is no separate CSS build or JavaScript application framework.

To inspect the Core design contract without starting the application, run:

```sh
go run ./tools/designexport
```

The command writes a JSON snapshot to standard output: stable example and
component identities, supported typed property values and schemas, named slot
support, rendered HTML, the matching CSS, theme tokens and SVG glyphs with
source and license information.
Font values are system fallback stacks; no font files are embedded. A content
hash lets a consumer detect whether its source snapshot has changed.

[ui.Export](ui/export.go) accepts a palette, explicitly bound examples and
optional stylesheet additions. A product uses that same API with its own
`design.Pair`, examples and `ui.Extra` values; it does not register a second
component catalog. Core retains the `pk-ui.component.*` identities as stable
component names, not references to additional repositories.

The snapshot is adapter input, not a `.fig` file, editable library or prototype.
These packages are the implemented UI system. They do not by themselves prove
an editable OpenPencil library, design-file round trips or end-to-end coverage
of a product flow.

## Use the HTTP API

Sessions use cookies. The reference application's login endpoint is
`POST /api/v1/auth/login`; authenticated resource routes live under
`/api/v1/<module>/<entities>`. Send the configured tenant host with every
request. The application rejects undeclared authorization requirements at boot.

`GET /health` and `GET /ready` report process and dependency readiness.
`GET /api/v1/admin/resources` describes registered resources for another
authorized shell. OpenAPI is available at `/openapi.json` when
`server.docs` is enabled. See [config.example.yaml](config.example.yaml) and
the owning module's routes for the exact configuration and access contract.

## Verify a change

From this repository, use the local test services and run:

```sh
make check
```

`make check` builds, vets and formats-checks Go, runs real-service tests, and
checks source budgets, package budgets, imports and tenant-setting ownership.
`make e2e` runs browser journeys and also needs Node, npm, `psql`, `curl`
and Playwright-managed Chromium. Install the browser dependencies once before
the first run:

```sh
npm --prefix e2e ci
npm --prefix e2e run install:browsers
```

The browser setup may require permission to install operating-system packages.
The test script recreates the fixed `platformkit_e2e` database on the configured
development server; confirm it is disposable and do not run this target
concurrently against that server. Once the prerequisites and disposable target
are confirmed, run `make e2e` with the same dependency-port settings.
Both checks run in CI. See [Makefile](Makefile) for the current targets and
[Contributing](CONTRIBUTING.md) for focused checks and review requirements.

Tests create and remove their own database schemas. Use development test
services, never production credentials. `make down` removes the local Compose
volumes as well as stopping services; it is a data deletion, not a test step.

## Release and support

[RELEASE.md](RELEASE.md) describes the reviewed tag and image workflow.
A successful build is not evidence of a deployed service; verify the image
digest and the receiving environment separately.
[CHANGELOG.md](CHANGELOG.md) records versioned changes,
[SECURITY.md](SECURITY.md) explains private vulnerability reporting, and
[LICENSE](LICENSE) and [NOTICE](NOTICE) record licensing and provenance.
