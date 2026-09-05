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

Use the existing checkout. A new installation needs Git, Docker with Compose,
and the Go version declared in [go.mod](go.mod). From this repository:

```sh
scripts/start.sh
```

The script starts local PostgreSQL and NATS, creates `config.yaml` if it is
missing, migrates the configured database, bootstraps the first tenant and
administrator, and serves the application. It preserves existing configuration.
The generated administrator password is printed once; keep it private.

Open [the local sign-in page](http://platformkit.localhost:8080/admin/login).
The request host selects the tenant, so use `platformkit.localhost`, not an
arbitrary host alias. The component gallery is at `/admin/_gallery`.

If you want setup without starting the server, use
`scripts/start.sh --no-run`, then run the launch command it prints. That command
includes the application environment overrides needed for nondefault ports;
`make run` alone does not translate Compose port settings into configuration.
Set `PLATFORMKIT_PG_PORT` and `PLATFORMKIT_NATS_PORT` when running the setup
script and tests. Check `docker compose ps` before starting another stack.
The example configuration is for local development, not a production deployment.

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
