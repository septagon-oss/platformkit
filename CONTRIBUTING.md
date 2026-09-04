# Contributing

Read [ARCHITECTURE.md](ARCHITECTURE.md) for the current implementation,
[AGENTS.md](AGENTS.md) for contribution rules and [docs/adr](docs/adr) for decisions.

    scripts/start.sh    # local dependencies, configuration, first tenant, app

Run make check for the required checks and make e2e for browser journeys.
Tests need real Postgres and NATS; make up starts the local dependencies.

## The shape of a change

A module owns a business capability: contracts/ defines its public behavior,
internal/ implements it, and module.go declares its dependencies and manifest.
Use modules/task to understand the current shape; define a new capability's
contracts for its own domain.

- Define contracts and independent conformance cases before implementation.
- Import other modules through their contracts; keep their implementation private.
- Keep business decisions separate from persistence and transport when that
  makes the rule easier to understand and test. Start within the owning module.
- Use the existing generated admin for record management. Give custom workflows
  an explicit surface and journey tests.
- Extend the existing composition path. Add no registry type, configuration
  namespace or generated document.
- Keep each business rule in one place. A real service and a fake can share pure
  decisions while independently specified cases check their behavior.

## Code should read like the business

Use the words people use for the capability: assign a task, renew a subscription,
settle a payment. Names should explain intent at the point where they are read.

- Make the normal path readable from top to bottom. Use early returns for
  refusals and errors; avoid clever expressions and deeply nested control flow.
- Name a helper for a meaningful domain concept or actual repetition. Avoid
  chains of helpers that make a reader chase trivial steps across files.
- Show inputs, time, IDs, state changes, transaction ownership and external
  effects explicitly. Do not hide required facts or services in context values.
- Keep decision functions deterministic and leave caller-owned values unchanged.
  Mutating newly owned local data is fine when it makes the code clearer.
- Use narrow types and contracts that state what a caller needs. Do not create
  a generic framework for a single behavior.
- Explain constraints and reasons in comments. Let clear code express the steps.
- Write tests around examples, refusals and invariants with independently chosen
  expected results. Keep database and provider failure tests at their boundaries.

A reviewer should be able to explain a changed command's inputs, decision,
state change, effects and failure behavior from the command and its relevant
rule. Record where a reader needed hidden knowledge or unrelated files.
Naming conventions, line counts and coverage percentages cannot prove this.

## Budgets and verification

make check runs build, vet, formatting, real-service tests, source/package
budgets and import/tenancy checks. make e2e exercises the browser. Both pass
before a push; the required make check passes before each commit.

Budgets keep the kernel, modules, UI and tests accountable for their cost.
They allow a useful capability or test to grow within its reviewed allocation.
Do not compress readable code or remove useful tests to make a count smaller.

When a change exceeds a ceiling:

1. Remove the implementation or duplication the change actually replaces.
2. Split unrelated responsibilities into independently green changes.
3. If the remaining cost is justified, make a separate owner budget commit
   before the implementation. Name the behavior or maintenance improvement,
   affected bucket, expected cost and evidence that will validate the benefit.

go run ./tools/locbudget --write lowers ceilings. Rebaselining with --round 100
can raise them and requires that owner review. CI rejects implementation pull
requests that raise source or package ceilings against main. This process
also applies to necessary regression tests and released-data compatibility.

## Commits and pull requests

Keep one logical change in one repository. Use a conventional subject in the
present tense, with no trailing period. Describe the behavior, its owner and
how it was verified; paste the real command output into the commit body.

For a behavior change, demonstrate a relevant failing case before its correction.
For a refactor, preserve the independent behavior tests and state what became
easier to follow, test or change. Include concurrency and rollback checks when
transactional behavior is affected. Track any necessary compatibility code's
consumer and removal condition.

Measure maintainability with a bounded change by an unfamiliar maintainer:
time to find the owner, time to a correct tested change, assistance and unrelated
owners touched. Measure reuse with different products using the same shared
implementation. Keep failed attempts and rework visible.

Never commit secrets, config.yaml, binaries or generated assets. By contributing
you agree your work is licensed under Apache-2.0. [LICENSE](LICENSE) covers the
tree; record third-party material in [NOTICE](NOTICE). Report security issues
through [SECURITY.md](SECURITY.md).
