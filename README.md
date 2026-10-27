# PlatformKit

PlatformKit is a Go foundation for building multi-tenant SaaS applications.
Compose business modules, generate administration screens from resource schemas,
and build custom workflows with typed web components.

The foundation includes tenant isolation, authentication, permissions, events,
background jobs and audit trails. Reference modules add users, notifications,
billing, content, files, sites and tasks. Your application chooses the modules
and connects their dependencies explicitly in Go.

## Try it locally

**Prerequisite: one thing — Go.** [go.mod](go.mod) states the floor on its `go`
line and the version this repository is verified with on its `toolchain` line; any
Go from 1.21 onwards reads the `toolchain` line and downloads that toolchain by
itself, which is a download of its own on a machine whose Go is older. Nothing
else: no Docker, no `psql`, no `make`, no Node.

**Command.** Run this from the directory where you want the application's data to
live. The first start downloads Go modules and then PostgreSQL, so it needs
internet access; a machine behind a filter is refused with the host named.

```sh
go run github.com/septagon-oss/platformkit/apps/platformkit@main start --addr 127.0.0.1:8080
```

**Machines.** `start` runs on `linux/amd64`, `linux/arm64`, `darwin/amd64` and
`darwin/arm64`, and refuses any other pair by name before it downloads or writes
anything. WSL2 answers as Linux and takes that answer; Windows is refused, not
because it is known to be broken but because no release has run it — use
`platformkit run` there, against a PostgreSQL of your own.

**Expected result.** A banner naming the reference and its version, the data and
cache directories, the addresses, and the next useful action — then a server on
the address you named. Everything answerable about your machine is answered before
the first byte is downloaded: an occupied `--addr`, a data directory that cannot be
written, a data directory another server is holding, or a cluster from another
PostgreSQL version is refused with the path or flag that would change it, the
command to retry, and the statement that nothing was written. Rerunning the same
command after such a refusal is safe, and it is also safe after a run that
succeeded: the same command reuses the database and the uploads under `./data`,
creates no second tenant and prints no second password.

```
  reference PlatformKit v1.1.2-proof.20261027.e5fe4025c5ad
  data      /you/platformkit            (uploads under /you/platformkit/files)
  database  /you/platformkit/postgres    (binaries under /you/.cache/platformkit/postgres)
  tenant    platformkit, administrator admin@platformkit.localhost
  password: <printed once, by the run that created the administrator>
            It is not stored and will not be shown again.
  site      http://platformkit.localhost:8080/
  admin     http://platformkit.localhost:8080/app/admin/login
  next      sign in at the admin address, or the two curl calls that follow
```

`start` runs its own PostgreSQL 16 and an initial tenant, so no separate database or message broker is
needed, and by default the HTTP server listens only on your machine, on
`127.0.0.1:8080`. Use `--data` to choose another data directory and `--addr` to
change the listening address and port — an address that names no host, such as
`--addr :8080`, binds every interface the machine has and answers the administrator's
sign-in page for whatever else is on that network, which is what you want only when
you meant it.

After signing in, explore the administration screens and the
[component gallery](http://platformkit.localhost:8080/app/admin/_gallery).
Product applications configure [tenant storybooks](modules/admin/README.md) to
show only the examples selected for the signed-in tenant. An optional
[Storybook.js adapter](ui/storybook/README.md) adds Storybook's native navigation
and controls while rendering these same Go components.

**Next useful action.** Open
[the sign-in page](http://platformkit.localhost:8080/app/admin/login) with
`admin@platformkit.localhost` and the password the banner printed — keep it, it is
shown only once, and it never appears in a log the run writes. Then create a task;
the banner prints the two `curl` calls that do it through the public API if you
would rather not use a browser. Use `platformkit.localhost` as the host because the
host identifies the tenant. Stop with Ctrl-C.

The measured cost of all of this, per machine, is in
[apps/platformkit/FIRST-USE.md](apps/platformkit/FIRST-USE.md) with the command
that produces it. This is a local evaluation setup, not a production configuration.
For a deployed application, pin a reviewed module version and supply your own
configuration, database and service credentials.
[config.example.yaml](config.example.yaml) describes the available settings; its
defaults are for local development.

## Build your application

Use PlatformKit as a versioned Go dependency and compose it with your own modules.
[The reference application](apps/platformkit/app.go) shows how to name the
modules you use and let them wire themselves by contract, and
[apps/platformkit/product.go](apps/platformkit/product.go) shows the share an
application owns: the values no module may decide. Start with
[the task module](modules/task/) when adding a capability: its public contracts
describe the behavior, while its implementation stays behind that boundary.

External services connect through the contracts their consumers require. For
example, the included billing provider records charges but does not transfer
money; connect a payment processor when your product needs to collect payments.
Email delivery likewise requires an SMTP configuration, and refuses registration
by email without one. From a checkout, `make up` starts a Mailpit sink beside
Postgres and NATS and `make run` points the application at it through
`PLATFORMKIT_MAIL_HOST`, `_PORT` and `_FROM`, so a development instance can sign
people up and read its own inbox (`PLATFORMKIT_MAILPIT_PORT`).

[Architecture](ARCHITECTURE.md) explains composition, tenant isolation,
authorization and migrations in more detail.

### Compose it in sentences

`pkit` is the composition layer above the kernel: one application, named, with
the modules it uses and the handful of things no kernel module can decide for
itself.

```go
app := pkit.NewApp("collect").Use(uses...).Theme(pair).Languages(copy).
	Home("/").ErrorPage(refusal).AskForAccess(reach, form).Roles(roles...)
return app.Run(ctx, deployment, app.All)
```

  - **Reused** — `kit/app` stays the engine: it builds the API and answers its
    gates, opens the connection, migrates, opens the transport and serves. `pkit`
    adds the dry registrations before all of it and nothing after it.
  - **Added** — the resolver over declared needs, provides and contributions, and
    every answer given before the first effect: no connection is opened, no
    migration runs, no store is dialled and no port is listened on until the
    composition resolves and its route gates — the whole composition mounted four
    times and compared against itself, the last of those being the API that would
    serve — have answered. Beside those answers the process keeps two records. Which
    application it runs on which database, and with which composition: a second
    application over a held database — hosted by another `pkit.Server` value or built
    straight from an `App` — or the held name coming back with a different list of
    modules is refused there, naming the application already standing. And the shapes
    a composition declares its events to have, which are process state, claimed above
    the connection: a boot beside a live application installs its declarations next to
    the standing ones, is refused if it spells one of them another way before it dials
    or migrates anything, and gives back what its own boot declared when its `Runtime`
    closes — or when a refusal of its routes leaves no `Runtime` to close. A refused
    build changes nothing in the process, nor the database it named.
  - **Added** — `App.Describe`, the same resolved plan as `Explain` rendered as
    a versioned document (`platformkit.composition.v1`): the build order, who
    supplies whose need, what `Choose` settled over whom, the implementation the
    deployment picked, the configuration section *names* each module read, and the
    problems a composition that does not resolve is refused for. It carries names
    only — no configuration value, no deployment input, no credential, no tenant
    row — and a refused composition describes no modules at all. It is a reading of
    your own composition, not a sandbox: the modules' trusted build functions run
    during it, as they do for `Validate` and `Explain`.
  - **Made reusable** — the reference application is composed this way in
    [apps/platformkit/app.go](apps/platformkit/app.go), and what it resolves to is
    committed as [COMPOSITION.development.md](apps/platformkit/COMPOSITION.development.md)
    and [COMPOSITION.production.md](apps/platformkit/COMPOSITION.production.md),
    written by `pkit.Server.Explain` and refused when it drifts, and beside them
    as [COMPOSITION.development.json](apps/platformkit/COMPOSITION.development.json)
    and [COMPOSITION.production.json](apps/platformkit/COMPOSITION.production.json),
    written by `pkit.App.Describe` on the same drift discipline: a composition
    changed in either encoding and not described fails a check.

Try the public APIs through the executable examples in [db](kit/db/example_test.go),
[events](kit/events/example_test.go), [httpx](kit/httpx/example_test.go),
[rest](kit/rest/example_test.go), [crud](kit/crud/example_test.go),
[cache](kit/cache/example_test.go), [jobs](kit/jobs/example_test.go) and
[tenancy](kit/tenancy/example_test.go), using the [test setup](CONTRIBUTING.md#verify-at-the-relevant-boundary).
**Reused** — these examples compose the existing kernel APIs, RLS fixtures and memory adapters.
**Added** — checked usage examples, a callback lifetime for database fixtures and an execution assertion fill the documentation gap without changing runtime APIs.
**Made reusable** — `dbtest.WithSchema` supports examples without a test handle, and `exampletest.Execute` verifies that Go runs each documented example.

## Build a screen

Start with the [entity and presentation map](ARCHITECTURE.md#entity-and-presentation-contracts)
to reuse generated forms and typed components, and the [presentation map](ui/README.md)
for what each `ui` package owns. Compose custom workflows with [pages](ui/page/);
shared [themes](design/README.md) change colors and typography across both
generated and custom screens.

[Page localization](ui/page/README.md) composes module-owned messages through
Go's x/text catalogs. The reference sign-in page includes English and Portuguese;
additional screens supply their own messages through the same page contract.

The web interface is server-rendered, with HTMX for interactions.
[ui.Compose](ui/ui.go) produces the stylesheet from shared components and your
own declarations; there is no separate CSS build step.

[Design tooling](tools/designexport/README.md) provides source snapshots and
experimental editor integration. It is not yet a complete editable design library
or a page-and-flow prototyping solution.

## Use the HTTP API

Sign in through `POST /api/v1/auth/login` and retain the session cookie for
authenticated requests. Resource routes live under `/api/v1/<module>/<entities>`;
send requests to the configured tenant host.

The local evaluation exposes [interactive API documentation](http://platformkit.localhost:8080/docs).
For a configured application, `/docs` and `/openapi.json` are available when
`server.docs` is enabled; they are public endpoints, so enable them deliberately.
`GET /api/v1/app/resources` describes the resources available to an authorized
shell. `GET /health` and `GET /ready` report process and dependency readiness.

A refusal arrives in one of two shapes, chosen by what the client asked for: a
program gets an RFC 9457 problem document, a browser a sentence, a request
reference to quote, and the way on the verdict leaves open. Same verdict, same
status; a refusal never becomes a page that says it succeeded.

## Contributing and project information

[Contributing](CONTRIBUTING.md) covers working from a checkout, test setup and
review requirements; [AGENTS.md](AGENTS.md) guides coding agents.

See the [changelog](CHANGELOG.md) for versioned changes and
[security policy](SECURITY.md) for private vulnerability reporting.
[LICENSE](LICENSE) and [NOTICE](NOTICE) contain licensing and attribution.
