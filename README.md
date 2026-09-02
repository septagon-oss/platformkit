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

Then open **<http://acme.localhost:8080/admin>** and sign in with the address
and the password `bootstrap` printed. That is the whole application: the
dashboard, a screen for every entity, and `/admin/_gallery` for every component
it is drawn with. The screens are generated from each entity's schema, so there
is one of them for `tasks` and `users` without a line of code for either.

The database starts empty. `bootstrap` migrates it, creates the first tenant,
the two roles a tenant starts with, and the administrator who signs in to it,
all in one transaction — and refuses to run again once any tenant exists, which
is what makes it safe to leave in the binary. The password is printed once, to
stderr, or taken from `PLATFORMKIT_BOOTSTRAP_PASSWORD`.

The same thing over the API, if a screen is not what you came for:

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
`check-loc`, `check-packages` and `check-gucs`. `make e2e` is the tenth gate:
it boots the application on a database of its own and drives the admin shell
with a browser. Against [ARCHITECTURE.md](ARCHITECTURE.md)'s ten gates, all ten
are real today. CI runs both targets.

## Status

Being extracted from a larger private codebase; see [docs/adr](docs/adr) and the
line budget in [loc-budget.json](loc-budget.json).
