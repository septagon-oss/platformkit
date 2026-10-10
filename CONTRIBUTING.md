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

Use the Go toolchain in [go.mod](go.mod), Make and Docker with Compose. The `go`
line there is the floor a consumer of this module has to clear; the `toolchain`
line is the version this repository is verified with. Make selects that exact
toolchain for its commands and child scripts, including the formatter; a different
installed Go does not change the verification version. The Go command downloads the
selected toolchain if it is not already available.
From the repository root, start the development PostgreSQL, NATS, Valkey, object
store and mail catcher services:

```sh
docker compose ps
make up
make check
```

Set `PLATFORMKIT_PG_PORT`, `PLATFORMKIT_NATS_PORT`, `PLATFORMKIT_VALKEY_PORT`,
`PLATFORMKIT_S3_PORT` and `PLATFORMKIT_MAILPIT_SMTP_PORT` /
`PLATFORMKIT_MAILPIT_PORT` if the default ports are in use, retaining those values
for every command. Never use production test credentials: tests create and remove
database schemas. `make down` deletes the Compose volumes as well as stopping
services; it is not a test step.
The Valkey service is optional in a way the others are not: `kit/cache` boots
without a store, so its adapter's conformance suite skips when nothing answers the
Valkey port and names the skip, and every other case in `make check` runs either way.
Exporting `PLATFORMKIT_TEST_VALKEY_URL` yourself runs those cases against what you
named, and a name that does not answer is a failure.
Use the [local setup](README.md#try-it-locally) for application development.
`make run` instead uses `config.yaml`, copied from `config.example.yaml` when absent.

`make trace` starts a third container: an OTLP collector that prints every span and
metric datapoint it receives to `docker compose logs collector`. Point
`telemetry.otlp_endpoint` at `localhost:4317` and the application you are working on
shows its own traces there, which is how a change to `kit/telemetry` is checked
without a trace backend. It sits behind a Compose profile rather than inside `make up`
because `make up` is what `make test` and `make check` rest on, and its image cannot
be pulled on an offline machine; `make down` stops it along with the others.
`PLATFORMKIT_OTLP_PORT` moves its port the same way the two above do.

`make test` uses pinned gotestsum with Go's package cache. Focus a case with
`make test TEST_PACKAGES=./modules/task/internal TEST_FLAGS='-run TestConcurrentTaskCommands'`.
`make test TEST_OPTIONS=--watch` waits for Go edits, then checks the selected
packages; keep the default `./...` to include consumers. Rerun explicitly after
file deletion, module, fixture or asset changes. For JSON/JUnit and slow-test
reports, use the gotestsum options documented beside `TEST_OPTIONS` in [Makefile](Makefile).
The cache cannot observe database or NATS state; after external-input changes,
run `make test TEST_FLAGS=-count=1`. See [native watch](tools/designexport/openpencil/README.md).

`make check` runs fresh tests across all packages regardless of local filters,
unless its caller empties `TEST_COUNT` (CI sets nothing, so CI always runs fresh),
plus build, vet, formatting, budgets, imports, version and tenant-setting checks.
`make check-race` runs the outbox, the request transaction, the advisory locks,
the limit counters and the router under the race detector. CI runs `check` and
`check-race`, so the detector is not something a contributor has to remember; it
is a separate goal because -race roughly doubles the suite.
`make e2e` adds browser journeys. Both pass before pushing; `make check`
passes before committing.

### Which tests run where (decision 0088)

Four tiers, and `make check` is the sum of them rather than one of them:

| tier | runs | what it must not run |
|---|---|---|
| `make check-push` | build, vet, formatting, the budgets and pins, and the Go cases of the packages a diff reaches that open no stack | anything that opens Postgres, a broker, the object store or the mail catcher — the selector refuses the selection rather than waiting for one |
| `make check-merge` | `make check-rehearse` and `make check`: the whole suite, the race suite beside it, and the composition journeys inside it | — |
| `make check-nightly` | `make e2e`, `make mobile-e2e`, `make flakes` | — |
| the inventory | `make check-test-inventory` re-derives [tests/inventory.json](tests/inventory.json) from the tree | a row with no test, a test with no row, a verdict outside the four, a `merge` naming nothing, a `delete` beside an unmeasured coverage column, a round-named count above its ceiling |

Selection is read out of that table, so no package is in a tier by one rule and
out of it by another. **Reused** — `tests/inventory.json` and
`tests/INVENTORY.md` (the specify round's ratified table), `scripts/check_packages.sh`'s
count-against-ceiling shape, `scripts/check_budget_ratchet_test.sh`'s fixture-tree
pattern for pinning a checker, `gotest.tools/gotestsum`'s `--jsonfile` and `tool
slowest` as already pinned in [go.mod](go.mod), `apps/platformkit`'s own harness
(`configure`/`install`/`compose`/`appOptions`/`start`/`signIn`/`do`) and
`trailIncluded` for the new journey, and `go list`'s dependency graph for the
diff's consumers — production importers and, since review 2, the packages whose
test binary compiles the change. **Added** — `scripts/test_inventory.py`, because the tree holds
no test register of any kind and 0088 asks for one CI can re-check; and
`apps/platformkit/tenancy_a_write_for_a_tenant_the_plan_does_not_open_is_refused_test.go`,
because the composition refused a plan-gated tenant only in prose until two tenants
walked one door in one boot. **Made reusable** — a tier selector any later task
can call (`scripts/test_inventory.py --tier push|merge|nightly --base <ref>`), a
flake report that turns `make flakes` into the source of the inventory's empty
`duration` and flake columns, and a fixture-tree pin shape for a checker over tests
that no longer needs a database to prove what it refuses.

Browser checks also require Node, npm, `psql`, `curl`, Playwright Chromium and `ss` or `lsof`.
The journeys that open a link the application mails read it from compose.yaml's
mailpit catcher, so [scripts/e2e.sh](scripts/e2e.sh) refuses a run with nothing
answering that address rather than letting the journey time out on a mail nobody
received; `make up` starts it. The two catcher ports name everything else:
[scripts/e2e.sh](scripts/e2e.sh) derives the address the application dials and the
address its journeys read from `PLATFORMKIT_MAILPIT_SMTP_PORT` and
`PLATFORMKIT_MAILPIT_PORT`, so a caller that runs one spec directly reaches the
catcher its own stack publishes rather than the default port. Install the browser
dependencies once, then run with the same service ports:

```sh
npm --prefix e2e ci
npm --prefix e2e run install:browsers
make e2e
```

Browser installation may require permission to install system packages. The [test
script](scripts/e2e.sh) removes its database and uploads, keeping failed Playwright
results at the printed path. A run picks a free loopback port, `PLATFORMKIT_E2E_PORT`
pins one; a taken pin and a port whose listener the run did not start are both refused.
[Makefile](Makefile) owns the commands; [RELEASE.md](RELEASE.md) the publication procedure.

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
separate owner budget commit before the implementation: a commit whose subject
begins `build(budget):` and which changes only `loc-budget.json` and
`packages-budget.json`. State the affected bucket, expected cost, benefit and
verification in its message. The ratchet accepts a raise only when every commit
on the branch that touches a budget file is such a commit, so the raise stands
alone in history; a removed bucket or a changed measurement is never accepted.
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
