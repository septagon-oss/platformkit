# Direct import boundaries

The shared entry point is [scripts/check_imports.sh](../../scripts/check_imports.sh):

```sh
scripts/check_imports.sh
scripts/check_imports.sh /path/to/product github.com/septagon-oss/platformkit
```

With no arguments the wrapper checks the foundation containing the script,
regardless of the working directory. An explicit repository is relative to the
caller. Following arguments declare dependency **module paths**, without versions.
Consumers run their resolved foundation's wrapper; they do not copy the checker.
The wrapper builds this private command in an external temporary directory and
preserves its exit status: 0 accepted, 1 policy refused, 2 input or discovery failed.

The graph comes from `go list -json -test ./...` in the target repository. The
caller's platform, tags, workspace and vendor selection apply. Only the target
main module's direct source imports are audited. There is no transitive walk and
no import-text scan. Standard-library identity comes from `go list`'s `std` set.
Incomplete output, Go errors and missing provenance fail closed. `-mod=mod` is
refused: repairing discovery does not authorize edits to dependency pins.

A module owner is its full Go module path plus `modules/<name>`. Ordinary source
may consume a peer's `contracts` tree. Constructors belong to composition;
foreign `internal` packages remain forbidden, including from tests. Go may refuse
internal visibility before the policy decision, returning status 2 with its
original diagnostic. Production imports of `contracts/<name-ending-test>` and
its descendants are refused even within one owner. Actual tests may use those
helpers; a package merely named `_test` gains no test permissions.

Production contracts additionally permit only ordinary module contracts, standard
value packages, and the exact kernel and third-party value/algorithm sets in
[policy.go](policy.go). SQL execution, HTML rendering, fixtures, implementations
and unknown external dependencies are refused. `net/http` and `database/sql/driver`
retain their standard vocabulary roles. This is a direct dependency policy;
permitted kernel packages can have implementation dependencies of their own.

What a released surface already published stays usable until a release can drop it.
Such an edge is recorded in the **checked repository's**
`scripts/imports-contract-baseline.txt`, one row per edge: the contracts package
directory, the exact dependency, and the exported name at the release line's frozen
commit that carries it. A row admits that one production contract import and
nothing else — an unrowed contract dependency, a foreign `internal` tree and a
test-helper import are refused beside it. A row whose import has gone is refused as
well, so the list can only shrink as each edge moves out. Every row is announced
once with the state the current graph gives it and its reason. A missing file
admits nothing, so a repository that never published such an edge keeps the rule in
full, and the accepted run says which dependencies the baseline rather than the rule
carries.

Go's `TestImports` and `XTestImports` identify test contributions, checked against
their compiled variants. Inherited production edges stay production. External
test packages retain their tested module owner. Rebuilt dependencies retain their
ordinary-source provenance; generated test executables contribute no source edges.

Test suites that compose foreign constructors need a row in the **target's**
`scripts/imports-integration.txt`:

```text
modules/a/internal@external example.test/product/modules/b Composes B migrations for A's service conformance.
```

The key is a package directory plus `@internal` or `@external`. Targets are exact
foreign constructor roots, comma-separated, followed by a nonempty reason.
Duplicates, globs, traversal, own roots and subpackage grants are refused. Missing
files mean no grants; malformed or unreadable files fail with status 2. Each row
is announced once with used and unused targets and its complete reason. An unused
grant is visible without refusing a build that excludes its suite. Grants never
waive production, internal visibility, test-helper or contract-purity rules.

## Module layers

A module belongs to one of four layers. The order — `platform`, `identity`,
`generic`, `product`, lowest rank first — is a compiled table in
[layers.go](layers.go), not content of a per-repository file: every repository
runs this gate, so a rank means the same thing in the kernel, the catalogue and a
product checkout, and no consumer can negotiate a fifth rank locally. Adding a
layer is a change to that table and a dependency repin.

| Rank | Layer | What belongs here |
| --- | --- | --- |
| 0 | `platform` | needs no account, no role and no person; works over a tenant id and values |
| 1 | `identity` | owns the account, the tenant record, a person's identity, or the channel the platform reaches a person through |
| 2 | `generic` | a reusable domain capability composed over those two, with no client name, sector rule, price or specialist fact |
| 3 | `product` | the module's reason is an application-facing surface: a workspace, a public site, a provisioning door, a person's own door |

This order is not ADR 0016's acceptance levels and not the CSS cascade layers
`scripts/check_ui_layers.sh` checks.

Each repository assigns **its own** modules in its own
`scripts/imports-layers.txt`, read from the checked repository and from every
declared dependency root, one module per row:

```text
github.com/septagon-oss/platformkit/modules/audit platform
github.com/septagon-oss/platformkit/modules/auth <-> github.com/septagon-oss/platformkit/modules/notification Both contract trees name each other's values, and neither direction moves without reshaping a published surface.
```

A row names one full module owner path of that repository and one known layer
name, with no reason: the reason a module sits where it sits is the table above
read against the measured graph in
[docs/MODULE-GRAPH.md](../../docs/MODULE-GRAPH.md). A `<->` row is the single
exception the file may carry — two modules whose production imports run both ways,
with the reason both stay. Both sides of that row name modules of the repository
whose file it is: no build measures a pair spanning two repositories, because Go
reports packages for the main module alone, so a pair that spans two is nobody's to
justify and each repository justifies only its own. A row naming a module of another
repository, an unknown
layer name, a duplicate, a subpackage, traversal or a `<->` row without a reason
returns status 2 with `file:line`: that is an unreadable declaration, never a
verdict. A **missing** file declares nothing, which is not an exemption.

Four rules read the map, and every refusal names both full package paths:

| Rule | Refuses |
| --- | --- |
| `module-layer-missing` | a module this build reaches with no row — own modules from Go's package list, so a module that imports nothing is still counted, and a dependency's modules from production import targets. The remedy names the owning repository's file: a map pinned before its own map is corrected upstream or by a new pin |
| `module-layer-upward` | a `production` edge whose source module ranks below the target's |
| `module-layer-cycle-undeclared` | a reciprocal production pair with no `<->` row |
| `module-layer-cycle-unused` | a `<->` row of this repository whose pair does not import each other in production here. The sweep reads the rows rather than the measured pairs, so a pair that holds no production edge in either direction is refused as well and the list can only shrink. It judges one build context: unlike an assignment row, which an excluded module leaves announced, a pair row here is refused when this run cannot see both directions, so a run over other platforms or build tags can read a genuine pair as unused — the map is judged in the context `./scripts/check_imports.sh` pins. A row read from a dependency's file is never judged here; it is announced as that repository's own verdict |

A declared port **prevents** an upward import; it never excuses one. The gate sees
a port as the absence of an edge — `kit/jobs.TenantLister` is provided by
`modules/tenant` and read by `modules/auth` and `modules/file` with no import
between them — so `decide` holds no port-aware branch, no waiver and no bypass:
no row, flag, environment variable or comment admits an upward production import.
The two legitimate cures are the remedy's: consume a port the lower module
declares, which removes the import, or assign the module the layer its real
dependencies already show.

Only `production` source is judged. Test contexts stay with
`scripts/imports-integration.txt`, which already demands an exact target and a
reason for every foreign constructor a suite composes; a layer rule over test
source would need an exemption for a row of this repository's own file. An unused
assignment row is announced, not refused, exactly as an unused integration grant
is: a build context that excludes a module is legitimate. A `<->` row read from a
dependency's file is announced too, and its notice names the repository whose own
gate measures that pair, because this build sees the main module's packages alone
and can say nothing about the other repository's directions.

**Reused** — the `graph` `normalize` already builds, the readable-declaration shape of
[grants.go](grants.go) and [baseline.go](baseline.go) with its parse-time refusals,
`decide`'s refusal set and remedy table, `edgeReport`'s JSON conventions, and the
`.gitattributes` + `make check-generated` guard that keeps a written file equal to
what its writer produces.
**Added** — the rank table, one repository's assignments and its justification rows,
and the four rules above: no data in this repository held a module layer before.
**Made reusable** — `--modules` and the `platformkit.module-graph.v1` report, so a
repository renders its own measured graph instead of keeping a map by hand, and the
order itself, so the catalogue and a product checkout rank their modules by the one
table rather than by a copy.

`kit/`, `pkit/`, `ui/`, `design/` and `apps/` hold no `modules/<name>` owner, so
the layer rule cannot rank them: `pkit` and `kit` sit under every layer, and those
trees keep their own boundaries — contract purity, `foreign-internal`, and the
transitive closure bounds `scripts/check_packages.sh` measures. In a vendored
build (`-mod=vendor`) a dependency's tree holds no readable declaration, which is
reported as status 2 naming the dependency rather than as an absent map; the
wrapper clears `GOFLAGS`, so the ordinary run resolves modules readonly.

For a measurement without a policy verdict:

```sh
go run ./tools/importgate --edges /path/to/product github.com/septagon-oss/platformkit
```

This writes `platformkit.import-edges.v1` JSON to stdout: sorted owner pairs, each
holding unique direct package edges and their sorted source contexts.

For the aggregate module graph instead — the nodes, the layer each module holds,
and the edges between modules with their computed direction:

```sh
go run ./tools/importgate --modules /path/to/product github.com/septagon-oss/platformkit
```

This writes `platformkit.module-graph.v1` JSON to stdout with the same exit
codes, and `scripts/module_graph.sh` renders it into
[docs/MODULE-GRAPH.md](../../docs/MODULE-GRAPH.md) from `make generate`. Its
`constructions` section holds the edges whose source is a module's own root
package, printed apart from `edges`: Go package imports cannot cycle while module
composition can, and the Granting construction between `modules/auth` and
`modules/user` is a fact about composition, not an import cycle. Direction is
computed, never declared, so an `upward` row can appear in a tree the gate accepts
— its contexts are then all test contexts. Allowed and
refused edges are included; own-owner, application and generated-main edges are
excluded. Pair size is a trend, never a budget. Diagnostics and grant notices go
to stderr. Measurement returns 0 even over policy violations, but discovery,
configuration and output failures still return 2. `go run` wraps nonzero program
statuses; use the built executable when distinguishing 1 from 2 matters.

Only this build context is measured. Other platforms, excluded tags, nested Go
modules and dependencies' own tests need their owners' separate invocations.
The caller must keep the checkout stable while Go reads it. The command does not
infer module tiers, inspect type aliases or validate API compatibility. Those
remain separate ownership and release checks.
