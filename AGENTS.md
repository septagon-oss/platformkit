# Working in PlatformKit

This repository owns the public runtime, reference modules, `design/`, `ui/`
and the reference application. Begin with [README.md](README.md), then follow
[CONTRIBUTING.md](CONTRIBUTING.md) for change criteria, checks and budget policy.

Inspect `git status --short --branch` and preserve existing changes. Before
proposing an abstraction, [trace an existing consumer](CONTRIBUTING.md#trace-an-existing-consumer).
[Architecture](ARCHITECTURE.md) owns the implementation map; start UI work at its
[entity and presentation contracts](ARCHITECTURE.md#entity-and-presentation-contracts).

Module contracts live in `contracts/` and implementations in `internal/`. Each
module's `provider.go` declares it to pkit and calls its one `New(Deps)`, and the
reference application composes modules in [apps/platformkit/app.go](apps/platformkit/app.go)
with `pkit.NewApp(...).Use(...)` ([ADR 0022](docs/adr/0022-pkit-orchestrates-composition.md)).

A production comment says what and why, in a few lines. It never cites a review, a
round, a finding, a task id or a `*_test.go` file name; `make check-comments` refuses
them. A doc comment states the contract (what, inputs, defaults, failure, lifetime);
longer reasoning goes to the package's `doc.go` or an ADR, linked.
Use the existing `design/` and `ui/` owners. Keep one logical change in this
repository; keep secrets, local configuration and generated artifacts out of commits.

Run `make check` before committing and `make e2e` before pushing, using the
development services described in [CONTRIBUTING.md](CONTRIBUTING.md#verify-at-the-relevant-boundary).
Read [Makefile](Makefile) before lifecycle commands; `make down` deletes test data.
Report actual results and unverified behavior. Do not weaken a failing check.

For documentation, verify links, source paths and commands. Update the canonical
explanation; label historical context and intended behavior explicitly.
