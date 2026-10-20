# 0022: pkit orchestrates composition

Status: accepted (decision 0074, 2026-10-01; decision 0091 D1, 2026-10-10). Supersedes [ADR 0002](0002-explicit-wiring.md).

## Problem

ADR 0002's hand-ordered `main.go` did not survive growth: in the main client
"a literal list" became a registry addressed by string, with module hooks
beside it. And a required `Deps` field was never a compile error — an omitted
field of a keyed struct literal compiles — so a module's required values always
needed validation that nothing performed before the first request.

## Decision

- An application is `pkit.NewApp(name).Use(modules...)`, and nothing is found
  that `Use` does not name.
- A module's `provider.go` declares it to pkit with `Provides`, `Needs`,
  `Optional`, `Contributes`, `After`, `FromDeployment` and `Describes`. Its
  build reads with `Get`, `All` and `Config`, writes with `Put`, and calls the
  module's one constructor, `New(Deps)`.
- The resolver checks the whole plan — duplicates, missing or ambiguous
  providers, cycles, phases, the deployment's inputs — before any build runs.
- Every build runs dry before anything opens; `kit/app.New` stays the engine
  (0074 rule 8).
- There is no `init()`, no registry addressed by string, and no reflection
  beyond a contract's type identity.
- A read a module did not declare, or a required read the plan cannot supply,
  stops that module's build at the call and refuses it by name, with the
  contract and its requiredness. A refused module's partial outputs are
  discarded, the modules that wait on it are reported as suppressed and owe no
  sentence, independent problems are still collected, and every other panic
  propagates unchanged. Each entry point returns a `pkit.CompositionError`
  (`errors.As`) except for `Plan`'s own name, settings, port and role checks.
- The resolved graph is reviewable: `Explain` writes it as sentences and
  `Describe` as values, committed per app as `COMPOSITION.<env>.{md,json}`.
