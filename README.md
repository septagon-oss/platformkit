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
    --tenant acme --host acme.localhost --name Acme --admin-email you@acme.localhost
make run
```

The database starts empty. `bootstrap` migrates it, creates the first tenant,
the two roles a tenant starts with, and the administrator who signs in to it,
all in one transaction — and refuses to run again once any tenant exists, which
is what makes it safe to leave in the binary. The password is printed once, to
stderr, or taken from `PLATFORMKIT_BOOTSTRAP_PASSWORD`.

Then sign in and do something, in a fifth command:

```sh
curl -sc jar -H 'Host: acme.localhost' -H 'Content-Type: application/json' \
    -d '{"email":"you@acme.localhost","password":"<the printed password>"}' \
    localhost:8080/api/v1/auth/login
curl -sb jar -H 'Host: acme.localhost' -H 'Content-Type: application/json' \
    -d '{"title":"chiller-2 supply temperature"}' \
    localhost:8080/api/v1/task/tasks
```

Every tenant is reached at its own host, so the `Host` header is what decides
whose data a request sees. Ports can be overridden with `PLATFORMKIT_PG_PORT` /
`PLATFORMKIT_NATS_PORT`.

## Gates

`make check` runs seven commands: `build`, `vet`, `fmt-check`, `test`,
`check-loc`, `check-packages` and `check-gucs`. Against
[ARCHITECTURE.md](ARCHITECTURE.md)'s ten gates that is eight of them real today
— `check-packages` counts an app now, and the empty-database boot test is in
`apps/platformkit` — and two waiting for the stage that gives them something to
check.

## Status

Being extracted from a larger private codebase; see [docs/adr](docs/adr) and the
line budget in [loc-budget.json](loc-budget.json).
