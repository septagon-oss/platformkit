# 1. One repository

Status: accepted, 2026-09-02. The public/private repository boundary is refined
by [ADR 0009](0009-what-is-public.md). Current package ceilings live in
[packages-budget.json](../../packages-budget.json).

## Context

PlatformKit was 67 git repositories in one workspace: a kernel, a module
catalog, a design system, a dozen `pk-*` libraries, client overlays, infra. A
change to a contract meant a commit in four repositories in a fixed order, and
2,122 packages were linked into the one binary that consumed them. The split
bought nothing: there was one consumer of almost every repository, and no
repository was released on its own cadence.

## Decision

One public repository, `github.com/septagon-oss/platformkit`, holds the kernel,
reference modules, UI stack and reference app in a single Go module. Shared
implementations live as ordinary packages under `ui/`, `design/` and `kit/`.
Commercial capabilities and client applications remain private consumers as
described in ADR 0009.

Package count is one reviewed cost of composition. The package gate checks the
ceiling in `packages-budget.json`; a new package must justify its ownership and
maintenance cost rather than exist only to bridge repositories.

## Consequences

- One `go.mod`, one `Makefile`, one CI workflow, one version.
- A contract change is one commit and one compile error set.
- Nothing enforces a boundary by version string any more, so the boundaries that
  remain (`contracts/`, `internal/`) are the ones the compiler can check.
- Independent release of a single library is no longer possible. No consumer
  wanted it.

## Evidence

```sh
make check-packages   # checks the current ceiling in packages-budget.json
```
