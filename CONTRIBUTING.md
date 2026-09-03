# Contributing

Read [ARCHITECTURE.md](ARCHITECTURE.md) first: the ten ideas, the ceilings, the
gates. [AGENTS.md](AGENTS.md) is the nine rules, and they apply to people too.
[docs/adr](docs/adr) says why.

    scripts/start.sh    # docker, go, Postgres, NATS, config, first tenant, run

Then `make check` for gates 1–9, and `make e2e` for gate 10 (needs node).

## The shape of a change

A module is three things and nothing else: `contracts/` (the entity, the events,
the permission tokens, the service interface, a fake and a conformance suite),
`internal/` (every implementation), and `module.go` (the manifest and a `Deps`
struct). Copy `modules/task`; it is the exemplar and it is short.

- **Contracts before implementations.** The `contracts/` package and its
  conformance suite exist and pass against the fake before `internal/` is filled.
- **Cross-module dependencies are interfaces** from the provider's `contracts/`.
  Importing another module's `internal/` is a gate failure, not a review comment.
- **A screen is generated from a schema.** A hand-written page earns itself in a
  comment saying why (ADR 0007). There are five in the whole application.
- **No new channel.** No new registry type, config key namespace or generated
  document. Fifteen make targets, ten gates, one config surface.
- **Close duplicates, never add them.** An interface is justified by a passing
  fake, not by a second production implementation.

## Gates

`make check` is gates 1–9 and is what CI runs on every pull request: `build`,
`vet`, `fmt-check`, `test`, `check-loc`, `check-packages`, `check-gucs` and the
import gate. `make e2e` is gate 10. Both are green before you push, and
`make test` needs a real Postgres — `make up` starts one.

`check-loc` is a ratchet: net lines go down. `go run ./tools/locbudget --write`
only lowers a ceiling. Raising one is an owner commit with a reason, and CI
fails a pull request that raises one by hand.

## Commits

- **One task, one commit, green build.** `make check` passes before you commit.
- **Delete the old path in the same commit that lands the new one.** No shim
  outlives its task; if the deletion will not fit, the task is wrong — split it.
- **Do not widen scope.** A defect outside the task is one line in an issue.
- **Verify before claiming.** Paste the real command output into the commit body.
- Conventional subject (`feat(kit):`, `fix(auth):`, `docs:`, `chore:`), present
  tense, one line, no trailing period.
- **Never commit** secrets, `config.yaml`, binaries or generated assets.

## Pull requests

One logical change. Say what you ran and paste what it printed. New behaviour
comes with a test that fails without it — for a module that is a case in the
conformance suite, so the fake and the real implementation are both held to it.

By contributing you agree your work is licensed under Apache-2.0. Do not add a
per-file copyright header: [LICENSE](LICENSE) covers the tree and anything
pulled from elsewhere is named in [NOTICE](NOTICE). Security issues go to
[SECURITY.md](SECURITY.md), never to an issue.
