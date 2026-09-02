# 1. One repository

Status: accepted, 2026-09-02

## Context

PlatformKit was 67 git repositories in one workspace: a kernel, a module
catalog, a design system, a dozen `pk-*` libraries, client overlays, infra. A
change to a contract meant a commit in four repositories in a fixed order, and
2,122 packages were linked into the one binary that consumed them. The split
bought nothing: there was one consumer of almost every repository, and no
repository was released on its own cadence.

## Decision

One public repository, `github.com/septagon-oss/platformkit`, holds the kernel,
the module catalog, the UI stack and the reference app in a single Go module.
The `pk-*` packages that are worth keeping fold into it as ordinary packages
under `ui/`, `design/` and `kit/`; the `pk-*` repositories are retired. Client
overlays and secrets stay in two private repositories.

Cost of a repository is paid in package count, not in repository count, so that
is what the gate measures: the app may link at most 400 first-party packages.
A module that needs a package earns it; a layer that exists to bridge two
repositories does not survive the merge.

## Consequences

- One `go.mod`, one `Makefile`, one CI workflow, one version.
- A contract change is one commit and one compile error set.
- Nothing enforces a boundary by version string any more, so the boundaries that
  remain (`contracts/`, `internal/`) are the ones the compiler can check.
- Independent release of a single library is no longer possible. No consumer
  wanted it.

## Evidence

```sh
make check-packages   # first-party packages linked into ./apps/platformkit <= 400
```
