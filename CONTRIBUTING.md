# Contributing

**How do I send a PR that won't be ignored?**

Thanks for wanting to contribute. This page tells you how to set up local
development, run the checks, and follow the conventions that keep a PR mergeable.
Follow these and your change reviews cleanly.

> **This repo is the front door.** The root `main` stays a thin wrapper around
> `pk-apps/pkg/starterapp` and remains domain-neutral. Application-owned modules
> and examples belong in the product repository that owns them; reusable
> framework behavior belongs in its PlatformKit layer (`pk-core`, `pk-modules`,
> `pk-runtime`, `pk-apps`, …).

## Local development: the multi-repo workspace

PlatformKit is several independently versioned repos (`pk-core`, `pk-modules`,
`pk-apps`, `pk-runtime`, `pk-tools`, …) that compose. To work across them at
once, clone the siblings into one directory and let a Go workspace (`go.work`)
resolve them from disk:

```bash
mkdir platformkit-workspace && cd platformkit-workspace

git clone https://github.com/septagon-oss/platformkit
git clone https://github.com/septagon-oss/pk-core
git clone https://github.com/septagon-oss/pk-shared
git clone https://github.com/septagon-oss/pk-runtime
git clone https://github.com/septagon-oss/pk-modules
git clone https://github.com/septagon-oss/pk-tools
git clone https://github.com/septagon-oss/pk-apps
# ...and any other layer you need to touch.
```

Then create a `go.work` at the workspace root listing the repos you cloned:

```bash
go work init ./platformkit ./pk-core ./pk-shared ./pk-runtime ./pk-modules ./pk-tools ./pk-apps
```

That is the whole setup. With the workspace in place, the per-repo builds and
tests resolve sibling modules from your local checkouts. Build and test each repo
from inside it (each is its own Go module) rather than running `go build ./...`
at the workspace root, e.g.:

```bash
for repo in platformkit pk-core pk-shared pk-runtime pk-modules pk-tools pk-apps; do
  ( cd "$repo" && go build ./... ) || break
done
```

> **Published modules carry no `replace` directives.** The `go.work` file is how
> sibling modules resolve *during local development*. Outside the workspace, each
> module resolves its dependencies by version from the Go module proxy. Do not
> add `replace` directives to a module's `go.mod` to make local dev work — that
> is exactly what `go.work` is for, and a `replace` would leak into published
> builds. If you find yourself reaching for `replace`, add the repo to your
> `go.work` instead.

## Running the checks

Each repo has its own checks. Run them from inside the repo you changed.

```bash
# This repo: build, test, and run the front door end to end
make verify         # format, vet, staticcheck, tests, race, front-door build
make coverage       # coverage report
make security       # govulncheck + gosec
make release-check  # complete release gate
go run .            # stable starter on 127.0.0.1:8080

# Per-repo build + tests (run from inside the repo you changed, e.g. pk-modules)
cd ../pk-modules
go build ./...
go test ./...
```

The `pk` CLI (in `pk-tools`) can sanity-check an environment and explain the
catalog. Run it from the `pk-tools` repo:

```bash
cd pk-tools
go run ./cmd/pk doctor           # checks your local toolchain/setup
go run ./cmd/pk verify           # runs verification for the layer
go run ./cmd/pk explain modules  # describes modules/contracts as data
```

`pk` is a dev-workflow tool — `doctor`, `verify`, `explain`. It does not run your
app; `go run` does.

## The invariants (these are not negotiable in review)

A PR that breaks one of these will be sent back. They are cheap to follow and
they are what keeps the architecture honest.

1. **No cross-module implementation imports.** A business module must never
   import another business module's package to reach its implementation. Depend
   only on interfaces — a shared port in `pk-modules/pkg/portslib` (like
   `AdminRegistrar`, `HealthRegistrar`) or another module's published contract
   (like `tenant.TenantService`). The wiring supplies the concrete type. If you
   need a capability another module has, depend on its interface, never its
   struct. See
   the [public documentation](https://github.com/septagon-oss/pk-docs) for the
   current pattern.

2. **Migrations are append-only.** Never edit an existing migration file. Add a
   new one with a higher sequence number (`0002_...`, `0003_...`). Someone has
   already run the old one.

3. **Every Go file declares its purpose (the C-14 convention).** Hand-authored
   source files carry exactly three adjacent leading lines. Tests use
   `Validates` in place of `Implements`:

   ```go
   // Implements: REQ-016.
   // Per: ADR-0017.
   // Discipline: C-14.
   ```

4. **Functional options are additive.** Never change the meaning of an existing
   `WithX` option; add a new one. Callers depend on the old behavior.

## Commits and PRs

- **Conventional commits, scoped to the repo.** Format:
  `type(scope): summary`. Examples:
  - `fix(pk-modules): map bad-input login to 400 instead of 500`
  - `feat(pk-apps): seed self-repairs the admin user on every boot`
  - `docs(pk-docs): add the add-a-module walkthrough`
  Types: `feat`, `fix`, `docs`, `test`, `refactor`, `chore`.
- **One repo per PR.** A change targets a single repo. If your work spans repos,
  open a PR per repo and link them.
- **Keep the diff focused.** A reviewer should be able to hold the change in
  their head. Split unrelated changes.
- **Run the checks before you push.** `make verify` is the pull-request bar;
  `make release-check` is the release bar.
- **Say what you changed and why.** A short PR body that states the problem and
  the fix beats a long one that restates the diff.

## Reporting bugs and proposing features

- Bugs and feature ideas: open a GitHub issue on the relevant repo.
- Security vulnerabilities are different — **do not** open a public issue. See
  [SECURITY.md](SECURITY.md).

---

See also the [PlatformKit documentation](https://github.com/septagon-oss/pk-docs)
for the current module and architecture guides.
