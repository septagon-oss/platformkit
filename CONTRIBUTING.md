# Contributing

[README.md](README.md) explains how to run the application.
[ARCHITECTURE.md](ARCHITECTURE.md) explains the implementation and ownership
boundaries. This guide describes how to make and review a change.

## Begin with the owning behavior

Inspect the working tree before editing. Locate the module, UI package or
runtime boundary that owns the task, then read its relevant contracts and
tests. Work in the existing checkout and preserve unrelated changes.

A module's `contracts/` defines its public behavior; `internal/` implements
it, and `module.go` declares its dependencies and manifest.
[modules/task](modules/task/) is the current example. Specify the new
capability's own contracts and independent conformance cases before filling
in its implementation. Import other modules only through their contracts.

Use generated screens for record management and explicitly composed pages
for custom interactions. Extend the existing composition path. Do not add
a parallel registry, configuration namespace or generated instruction set.
Keep a business decision in one implementation; a fake may share pure
decisions with it, but its conformance cases need independently specified
expected results.

## Make the change readable

Use domain language and explicit control flow. A reader should be able to
follow the normal path from inputs through a decision to its state change,
effects and failure behavior. Show transaction ownership, time, IDs and
external services at the point where they matter. Avoid hidden dependencies
in context values or mutable globals.

Introduce a helper when it names a meaningful concept or removes real
repetition. Avoid chains of trivial wrappers that make a reader search across
files. Keep decision functions deterministic and caller-owned values
unchanged. Comments should explain a constraint or reason; the code should
express the steps.

Remove the implementation a change replaces. Preserve established data and
upgrade contracts: the clean-baseline policy in
[ADR 0011](docs/adr/0011-migration-ownership.md) is not permission to discard
an installation's applied migration history.

## Verify at the relevant boundary

Use the Go version in [go.mod](go.mod), Make and Docker with Compose.
From the repository root, start the development PostgreSQL and NATS services:

```sh
docker compose ps
make up
make check
```

Set `PLATFORMKIT_PG_PORT` and `PLATFORMKIT_NATS_PORT` if the default ports are
in use, retaining those values for every command. Never use production test
credentials: tests create and remove database schemas. `make down` deletes the
Compose volumes as well as stopping services; it is not a test step.
For application development, `go run ./apps/platformkit start --addr 127.0.0.1:8080`
uses the [local setup](README.md#try-it-locally) with its own database.
`make run` instead uses `config.yaml`, copied from `config.example.yaml` when absent.

`make check` runs build, vet, formatting, real-service tests, source and
package budgets, imports and tenant-setting checks.
`make e2e` adds browser journeys. Both pass before pushing; `make check`
passes before committing.

Browser checks also require Node, npm, `psql`, `curl` and Playwright Chromium.
Install the browser dependencies once, then run with the same service ports:

```sh
npm --prefix e2e ci
npm --prefix e2e run install:browsers
make e2e
```

Browser installation may require permission to install system packages.
The [test script](scripts/e2e.sh) creates and removes its own database, uploads
and temporary browser artifacts. Concurrent runs need distinct
`PLATFORMKIT_E2E_PORT` values. [Makefile](Makefile) owns the command definitions;
[RELEASE.md](RELEASE.md) describes the separate publication procedure.

For a behavior change, demonstrate the failing case and its correction.
For a refactor, retain the independent behavior tests and explain what
became easier to follow, test or change. Exercise rollback and concurrency
when transactional behavior changes. A missing service or skipped browser
run is an unverified gate, not a successful one.

For documentation, check links, paths and command definitions. Keep README
customer-facing: purpose, prerequisites, first use and next steps. Verification
belongs here, tooling instructions beside their owner, architecture in
ARCHITECTURE and agent navigation in AGENTS. Maintain one canonical explanation.
Label intent, historical context and verified behavior distinctly.

## Keep budgets honest

[loc-budget.json](loc-budget.json) and
[packages-budget.json](packages-budget.json) hold reviewed ceilings.
A useful capability or test may grow within its allocation. Do not compress
readable code, delete useful tests or weaken a gate to fit a count.

If a change exceeds a ceiling, first remove what it replaces and separate
unrelated responsibilities. If the remaining cost is justified, obtain a
separate owner budget commit before the implementation. State the affected
bucket, expected cost, benefit and verification.
`go run ./tools/locbudget --write` lowers ceilings; rebaselining with
`--round 100` can raise them and requires that review.

## Hand off the result

Keep one logical change in one repository. Use a conventional commit subject
and explain the changed behavior, its owner and how it was verified. Include
the actual check output in the commit body. State remaining failures or
untested behavior instead of claiming the wider system is ready.

Never commit secrets, local configuration, binaries, dependency trees or
generated test artifacts. [LICENSE](LICENSE) covers this repository;
[NOTICE](NOTICE) records third-party provenance. Preserve required attribution
and report vulnerabilities through [SECURITY.md](SECURITY.md).
