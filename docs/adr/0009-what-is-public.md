# 9. What is public, and what the seam between the three repositories is

Status: accepted, 2026-09-03

## Context

ADR 0001 said one repository, and it was right about the thing it was arguing
with: sixty-seven repositories for one binary bought nothing. But "one
repository" is not the whole answer, because not everything in the old workspace
is publishable and not everything publishable belongs to the same audience.

Three kinds of thing came out of the extraction and they have three different
readers. A reference architecture is read by anybody, and its value is that it
can be run and copied. A competitive module catalog is read by the people who
sell it. A client's configuration, brand and data are read by that client and
nobody else. One repository holding all three would be private, and a private
reference architecture is a contradiction.

The number that decides where the seam goes is not "how many repositories" but
"how many compile-time couplings cross one". ADR 0001's answer stands for
everything inside the public module: package count, not repository count, is
what a boundary costs. What follows is where the two boundaries that remain are,
and why each one is worth the `go get` it costs.

## Decision

**Three repositories, and the seam between them is a Go module boundary and a
migration-number range.**

| Repository | Visibility | Holds |
|---|---|---|
| `github.com/septagon-oss/platformkit` | **public** | `kit/` (the kernel), `ui/` + `design/` (the browser half), `apps/platformkit` (the reference binary), `tools/`, `migrations/`, and eleven reference modules: task, tenant, user, auth, admin, audit, notification, billing, content, site, file |
| a private catalog repository | private | the commercial modules, importing the public module by tag |
| a private clients repository | private | per-client overlays (configuration and assets, no Go), client modules, the application that composes public + catalog + client into one image, and the cluster state |

The two private repositories are described and not named, here and everywhere
else in this repository, and that is the decision rather than an omission. What
they are called, what modules they hold and which organisation owns them are all
things a reader of a public repository has no need for and a competitor has a
use for. The seam is the interesting part, and the seam is the same whatever the
consumers are called.

The eleven public modules are the set a reference architecture needs to be
credible rather than the set that is easiest to give away. Each is the exemplar
of an idea somebody would otherwise have to invent: `task` is the shape,
`tenant` is the control plane, `auth` is sessions and passwords, `admin` is the
generated screens, `audit` and `notification` are what a subscription is for,
`billing` is a recurring charge, `content` and `site` are a public surface,
`file` is an upload. A twelfth module that taught nothing new would be catalog.

**What is never published, in any form, at any time:**

- **Client data.** Rows, backups, exports, screenshots with real names in them.
- **Catalog modules.** The commercial modules and their tests, fakes and
  migrations — including their names. A public repository that listed them in a
  comment would be telling a competitor the shape of the roadmap, and a comment
  is exactly where that leaks: this ADR named two of them until the release
  review counted the lines between the rule and the breach.
- **Client names and overlays.** Per-client directories are the private
  repository's, and this one uses `platformkit` as its own example tenant
  precisely so that no client has to be one.
- **Cluster state.** Manifests, hosts, LAN addresses, the GitOps repository.
- **Secrets.** Any secrets repository or store, by name or by contents; any
  `config.yaml` (it is git-ignored, and `config.example.yaml` is what is
  committed); any key.
- **The organisation the private repositories live in.** A URL is a name.

The public repository is therefore not a subset of the private ones with the
private parts removed. It was written to be public, and the two private
repositories depend on it — never the other way round. There is no import from
`github.com/septagon-oss/platformkit` to anything private, and the compiler is
what enforces that: the public module does not require either of the others.

### The `replace` directive has a lifecycle, and it ends here

Before the public module is tagged there is nothing to `go get`, so both private
repositories resolve it from a sibling checkout:

    replace github.com/septagon-oss/platformkit => ../platformkit

That directive is scaffolding with a defined end. `v1.0.0` is the end: the tag
is pushed, each private repository deletes its `replace` line, runs `go get
github.com/septagon-oss/platformkit@v1.0.0`, and keeps only the `require`. The
runbook for that lives in each private repository's own `RELEASE.md`, because it
is a thing done to a private repository and a public one has no business
describing somebody else's tree.

Afterwards the rule is: **a `replace` directive pointing at a sibling checkout
is a development convenience and never a committed state.** A private repository
that needs an unreleased public change either waits for a tag or uses an ignored
`go.work`, which is per-machine and cannot be pushed by accident. The reason is
not tidiness — it is that a `replace` makes the private repository's CI depend on
a directory that only exists on one laptop, and that failure appears as a green
build that proved nothing.

### Migration numbers are global across the three repositories

`kit/app` merges the public `migrations/` directory with every composed module's
own `Migrations fs.FS` into one flat set, applies it with one ledger, and
**refuses at boot a version that two sources both claim, naming both.** So the
version number is global across three repositories that never see each other's
files, and the ranges are what keep them apart:

| Repository | Versions |
|---|---|
| `septagon-oss/platformkit` | **1 – 999** |
| the private catalog | 1001 – 1999 |
| the private clients repository | 2001 upward |

This repository owns 1–999 and allocates from the bottom; it is at `000022`. The
gaps between the ranges are deliberate: a range that ended where the next began
would make the first over-allocation collide instead of failing to reserve.

Two consequences follow and both are rules rather than advice. **Files are
append-only from v1.0.0**: before it, a migration is the schema this repository
would create today and correcting one in place is cheaper than shipping a
history nobody ran; after it, somebody has run it. And **a public migration
number is never reused**, even for a module that is deleted, because the private
repositories cannot see that it became free.

### The ceilings are a published commitment

`loc-budget.json` is in the repository, `make check-loc` runs on every pull
request, and CI additionally fails a pull request that *raises* a ceiling. That
was an internal discipline while the repository was private. Publishing turns it
into something a reader can hold the project to:

The numbers live in `loc-budget.json` and `packages-budget.json`, and they are
ratcheted at every tag, so a table here would be a second copy that goes stale
the first time somebody deletes something — which is what happened: this ADR
published 15,000 / 150,000 / 40,000 while the file shipped a fifth of that. Read
the files, or run `make check-loc`. At v1.0.0 they were:

| Bucket | Ceiling | Count at v1.0.0 |
|---|---|---|
| `kit/` (kernel) | 8,200 | 8,106 |
| `modules/` | 11,700 | 11,626 |
| `ui/` + `design/` | 9,600 | 9,514 |
| `apps/` | 500 | 492 |
| `tools/` | 400 | 303 |
| total production Go | 29,900 | 29,850 |
| test support Go (`*test` packages) | 5,600 | 5,530 |
| test Go | 18,200 | 18,167 |
| browser JavaScript | 300 | 218 |
| markdown | 1,600 | 1,525 |
| first-party packages linked into the app | 54 | 54 |

The claim being made is narrow and checkable: *this* is what the architecture
costs, and a release that costs more will have said so in a commit that raised a
number, signed by the owner, with a reason. `RELEASE.md` makes the ratchet a
release precondition, so every tag republishes the commitment at the tightest
number the tree can honestly carry — the count rounded up to the next hundred,
which is small enough to be a ceiling and round enough that nobody negotiates
it.

## Consequences

- A contributor to the public repository can run, test and release everything
  they can see. Nothing in `make check` or `make e2e` reaches a private module.
- The two private repositories are pinned consumers. A public change reaches
  them at a tag, and a tag is the only thing that moves them, so a breaking
  kernel change is visible as a version bump in two `go.mod` files rather than
  as a surprise.
- **The public repository cannot be tested against the catalog.** Nothing public
  imports anything private, so the compatibility of the eleven public modules
  with a consumer's is proved in that consumer's CI and not here. That is the
  cost of the seam and it is paid by the private side.
- **Extraction is one-way.** A module that starts public stays public: the
  history is public, so making it private later removes it from the tree and
  from nowhere else. Deciding a module is catalog is a decision made before it
  lands, not after.
- A number allocated in one repository's migration range is spent for all three.
  Two modules written in parallel in the same repository still need the range
  written down; the private repositories keep a `migrations/README.md` table for
  that, and this one keeps the numbers in the filenames of one directory.

## Evidence

```sh
# The public module requires nothing private, and replaces nothing.
grep -E '^replace|^\trequire' go.mod | grep -v septagon-oss   # no private module

# One ledger, one directory, versions 1-999 and no duplicates.
ls migrations/*.up.sql | sed 's#.*/\([0-9]*\)_.*#\1#' | sort | uniq -d   # no output

# The ceilings hold, and CI fails a pull request that raises one.
make check-loc
```
