# PlatformKit

Reference architecture for composable, multi-tenant SaaS in Go: one binary,
explicit wiring, Postgres row-level security, generated admin screens, ten CI
gates. Read [ARCHITECTURE.md](ARCHITECTURE.md).

## Run it

The kernel is here; the reference binary it runs is stage E2, so today the five
commands are four and the last one is `make check` rather than `make run`.

```sh
git clone https://github.com/septagon-oss/platformkit && cd platformkit
make up
cp config.example.yaml config.yaml
make check
```

Ports can be overridden with `PLATFORMKIT_PG_PORT` / `PLATFORMKIT_NATS_PORT`.

## Gates

`make check` runs seven commands: `build`, `vet`, `fmt-check`, `test`,
`check-loc`, `check-packages` and `check-gucs`. Against
[ARCHITECTURE.md](ARCHITECTURE.md)'s ten gates that is seven of them real today
— `check-packages` passes trivially until there is an app to link — and three
waiting for the stage that gives them something to check.

## Status

Being extracted from a larger private codebase; see [docs/adr](docs/adr) and the
line budget in [loc-budget.json](loc-budget.json).
