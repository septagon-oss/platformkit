# Contributing

[README.md](README.md) explains how to run the application.
[ARCHITECTURE.md](ARCHITECTURE.md) explains the implementation and ownership
boundaries. This guide describes how to make and review a change.

## Trace an existing consumer

Before architecture, consolidation or dependency replacement work, trace one
existing consumer. For UI, start at the
[entity and presentation map](ARCHITECTURE.md#entity-and-presentation-contracts).

1. Resolve the consumer's actual dependency and composition, including local
   replacements. Inspect that revision; a sibling checkout may differ from what
   the application uses.
2. Follow the contract through its implementation, registration and caller to
   the visible result. Read the relevant tests. Confirm names and comments against
   the wired path before claiming a capability exists or is missing.
3. Run a focused test or small reproduction at that boundary. Identify the behavior
   existing code cannot provide, then compare reuse, extension and an external
   dependency before proposing another abstraction.

Record the resolved revision, owner, consumer, check result and specific gap in
the review or design note. Distinguish implemented, planned and unverified behavior;
state the search scope when reporting something absent.

Specify contracts and independent conformance cases before implementation. Reuse
the existing composition and [module boundaries](ARCHITECTURE.md#start-at-the-composition).
Do not add parallel registries, configuration namespaces or instruction sets.

Public packages must solve a named problem outside PlatformKit and simplify an
existing PlatformKit consumer. Apply the trace above and record its code,
dependencies or composition steps before and after the change. Keep one
implementation: migrate the traced caller and remove the replaced logic in the
same dependent delivery.
A compatible alias or delegate may preserve an existing public API. Publication,
moving files and synthetic examples do not establish adopted consumer reduction.
Show ordinary versioned consumption with an [executable public example](ui/forms/testdata/standalone/README.md),
explicit optional providers, dependency limits, error behavior and compatibility
expectations. Reuse existing checks and identify any pending migration.
[ADR 0012](docs/adr/0012-independent-parts.md) records the decision and its rationale.

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
Make selects that exact Go toolchain for its commands and child scripts, including
the formatter; an installed newer Go does not change the verification version.
The Go command downloads the selected toolchain if it is not already available.
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
Use the [local setup](README.md#try-it-locally) for application development.
`make run` instead uses `config.yaml`, copied from `config.example.yaml` when absent.

`make test` uses pinned gotestsum with Go's package cache. Focus a case with
`make test TEST_PACKAGES=./modules/task/internal TEST_FLAGS='-run TestConcurrentTaskCommands'`.
`make test TEST_OPTIONS=--watch` waits for Go edits, then checks the selected
packages; keep the default `./...` to include consumers. Rerun explicitly after
file deletion, module, fixture or asset changes. For JSON/JUnit and slow-test
reports, use the gotestsum options documented beside `TEST_OPTIONS` in [Makefile](Makefile).
The cache cannot observe database or NATS state; after external-input changes,
run `make test TEST_FLAGS=-count=1`. See [native watch](tools/designexport/openpencil/README.md).

`make check` always runs fresh tests across all packages, regardless of local
filters, plus build, vet, formatting, budgets, imports, declared-version and
tenant-setting checks.
`make check-race` runs the outbox, the request transaction, the advisory locks,
the limit counters and the router under the race detector. CI runs `check` and
`check-race`, so the detector is not something a contributor has to remember; it
is a separate goal because -race roughly doubles the suite.
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
The [test script](scripts/e2e.sh) removes its database and uploads, retaining failed
Playwright results at the printed temporary path. Concurrent runs need distinct
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
`--round 100` can raise them and requires that review. A branch that expects an
acceptance round prices that too, because the round arrives with a test file of
its own after the feature's lines are counted: re-baseline the bucket it writes
(`go_test`) with `--round 100 --allow 500`, 500 being the measured cost — the
largest acceptance-round test file this repository holds is 499 lines.

## Hand off the result

Keep one logical change in one repository. Use a conventional commit subject
and explain the changed behavior, its owner and how it was verified. Include
the actual check output in the commit body. State remaining failures or
untested behavior instead of claiming the wider system is ready.

Never commit secrets, local configuration, binaries, dependency trees or
generated test artifacts. [LICENSE](LICENSE) covers this repository;
[NOTICE](NOTICE) records third-party provenance. Preserve required attribution
and report vulnerabilities through [SECURITY.md](SECURITY.md).
