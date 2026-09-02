# PlatformKit

Reference architecture for composable, multi-tenant SaaS in Go: one binary,
explicit wiring, Postgres row-level security, generated admin screens, ten CI
gates. Read [ARCHITECTURE.md](ARCHITECTURE.md).

## Run it

```sh
git clone https://github.com/septagon-oss/platformkit && cd platformkit
make up
cp config.example.yaml config.yaml
make run
curl -H 'Host: platformkit.localhost' localhost:8080/ready
```

The database starts empty; the app migrates it and serves. `config.example.yaml`
ships a `dev:` block that stands in for the tenant and auth modules until stage
E3: `platformkit.localhost` is a tenant and every caller is an administrator of
it. It is refused unless `server.public_host` is a local name.

Ports can be overridden with `PLATFORMKIT_PG_PORT` / `PLATFORMKIT_NATS_PORT`.

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
