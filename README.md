# PlatformKit

PlatformKit is a Go foundation for building multi-tenant SaaS applications.
Compose business modules, generate administration screens from resource schemas,
and build custom workflows with typed web components.

The foundation includes tenant isolation, authentication, permissions, events,
background jobs and audit trails. Reference modules add users, notifications,
billing, content, files, sites and tasks. Your application chooses the modules
and connects their dependencies explicitly in Go.

## Try it locally

Install the Go version declared in [go.mod](go.mod), then run this command from
a directory where you want to keep the application's data. The first start
needs internet access to download dependencies and PostgreSQL:

```sh
go run github.com/septagon-oss/platformkit/apps/platformkit@main start --addr 127.0.0.1:8080
```

This starts the reference application from `main`, with its own PostgreSQL 16
database and an initial tenant. No separate database or message broker is needed.
The HTTP server listens only on your machine.

Open [the sign-in page](http://platformkit.localhost:8080/admin/login) and use
`admin@platformkit.localhost` with the password printed in your terminal.
Keep that password: it is shown only when the administrator is first created.
After signing in, explore the administration screens and the
[component gallery](http://platformkit.localhost:8080/admin/_gallery).
Product applications configure [tenant storybooks](modules/admin/README.md) to
show only the examples selected for the signed-in tenant.
An optional [Storybook.js adapter](ui/storybook/README.md) adds Storybook's native
navigation and controls while rendering these same Go components.

Use `platformkit.localhost` in your browser because the host identifies the
tenant. Stop with Ctrl+C; starting again from the same directory reuses your
database and uploads under `./data`. Use `--data` to choose another data directory
or `--addr` to change the listening address and port.

This is a local evaluation setup, not a production configuration. For a deployed
application, pin a reviewed module version and supply your own configuration,
database and service credentials. [config.example.yaml](config.example.yaml)
describes the available settings; its defaults are for local development.

## Build your application

Use PlatformKit as a versioned Go dependency and compose it with your own modules.
[The reference application](apps/platformkit/modules.go) shows how to connect
module constructors and their typed dependencies. Start with
[the task module](modules/task/) when adding a capability: its public contracts
describe the behavior, while its implementation stays behind that boundary.

External services connect through the contracts their consumers require. For
example, the included billing provider records charges but does not transfer
money; connect a payment processor when your product needs to collect payments.
Email delivery likewise requires an SMTP configuration.

[Architecture](ARCHITECTURE.md) explains composition, tenant isolation,
authorization and migrations in more detail.

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
`GET /api/v1/admin/resources` describes the resources available to an authorized
shell. `GET /health` and `GET /ready` report process and dependency readiness.

## Contributing and project information

[Contributing](CONTRIBUTING.md) covers working from a checkout, test setup and
review requirements; [AGENTS.md](AGENTS.md) guides coding agents.

See the [changelog](CHANGELOG.md) for versioned changes and
[security policy](SECURITY.md) for private vulnerability reporting.
[LICENSE](LICENSE) and [NOTICE](NOTICE) contain licensing and attribution.
