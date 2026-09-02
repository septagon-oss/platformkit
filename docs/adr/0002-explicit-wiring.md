# 2. Explicit wiring, no dependency-injection container

Status: accepted, 2026-09-02

## Context

The previous codebase composed modules with `uber/fx` behind a 34-field module
descriptor and eleven layers of materialization (~5,900 lines) between a
module's `module.go` and `fx.New`. It had 32 distinct fx groups and 91 registry
types. Wiring errors surfaced at boot, as reflection errors naming interfaces
rather than call sites, and reading the graph meant running the program.

## Decision

`apps/platformkit/main.go` constructs modules in dependency order and passes
each one a struct of typed dependencies. No container, no groups, no registries,
no reflection. A module's dependencies are the fields of its `Deps` struct; a
missing or mistyped one is a compile error at the construction site.

Cross-module dependencies remain interfaces declared in the provider's
`contracts/` package, so a module still depends on a capability rather than an
implementation, and a fake still satisfies the same conformance suite as the
real thing. What is deleted is the machinery that used to connect them.

## Consequences

- The wiring graph is readable top to bottom in one file, and `go build` checks it.
- Startup order is written down instead of derived, which makes it reviewable
  and makes cycles impossible to express.
- `main.go` grows with the catalog: roughly one line per module. That is the
  price of the graph being visible, and the `apps/` line ceiling caps it.
- Lifecycle hooks (start/stop) are ordinary method calls the kernel makes, not
  framework callbacks.

## Evidence

```sh
grep -r 'go.uber.org/fx' go.mod   # no output: the dependency does not exist
```
