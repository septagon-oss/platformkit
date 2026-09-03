# PlatformKit

Reference architecture for composable, multi-tenant SaaS in Go: one binary,
explicit wiring, Postgres row-level security, generated admin screens, ten CI
gates. Read [ARCHITECTURE.md](ARCHITECTURE.md).

## Run it

```sh
git clone https://github.com/septagon-oss/platformkit && cd platformkit
make up
cp config.example.yaml config.yaml
go run ./apps/platformkit bootstrap --config config.yaml \
    --tenant platformkit --host platformkit.localhost \
    --name PlatformKit --admin-email admin@platformkit.localhost
make run
```

Or `scripts/start.sh`, which is those five with the checks a first run would
otherwise discover one failure at a time — docker, the Go version `go.mod` asks
for, both published ports, the configuration file, the first tenant — and which
is safe to run twice.

Then open **<http://platformkit.localhost:8080/admin/login>** and sign in with
that address and the password `bootstrap` printed. Behind it is the whole
application: the dashboard, a screen for every entity, and `/admin/_gallery` for
every component it is drawn with. The screens are generated from each entity's
schema, so there is one of them for `tasks` and `users` without a line of code
for either.

The database starts empty. `bootstrap` migrates it, creates the first tenant,
the two roles a tenant starts with, and the administrator who signs in to it,
all in one transaction — and refuses to run again once any tenant exists, which
is what makes it safe to leave in the binary. The password is printed once, to
stderr, or taken from `PLATFORMKIT_BOOTSTRAP_PASSWORD`.

The same thing over the API, if a screen is not what you came for:

```sh
curl -sc jar -H 'Host: platformkit.localhost' -H 'Content-Type: application/json' \
    -d '{"email":"admin@platformkit.localhost","password":"<the printed password>"}' \
    localhost:8080/api/v1/auth/login
curl -sb jar -H 'Host: platformkit.localhost' -H 'Content-Type: application/json' \
    -d '{"title":"chiller-2 supply temperature"}' \
    localhost:8080/api/v1/task/tasks
```

Every tenant is reached at its own host, so the `Host` header is what decides
whose data a request sees. Ports can be overridden with `PLATFORMKIT_PG_PORT` /
`PLATFORMKIT_NATS_PORT`.

## Gates

`make check` runs seven commands: `build`, `vet`, `fmt-check`, `test`,
`check-loc`, `check-packages` and `check-gucs`. `make e2e` is the tenth gate:
it boots the application on a database of its own and drives the admin shell
with a browser. Against [ARCHITECTURE.md](ARCHITECTURE.md)'s ten gates, all ten
are real today. CI runs both targets.

## Contributing, security, releases

[CONTRIBUTING.md](CONTRIBUTING.md) is the shape of a change and the commit
rules. [SECURITY.md](SECURITY.md) is where a vulnerability goes — privately,
never an issue. [RELEASE.md](RELEASE.md) is the tag procedure and what the tag
builds.

## Status

Extracted from a larger private codebase, and complete: the kernel, the browser
half, the reference binary and eleven modules, all running. Not yet tagged —
`v1.0.0` is the first release, and [RELEASE.md](RELEASE.md) is what has to be
true before it. [ADR 0009](docs/adr/0009-what-is-public.md) says what this
repository holds and what the two private ones do; [docs/adr](docs/adr) says why
the rest of it is the way it is, and [loc-budget.json](loc-budget.json) is what
it costs.
