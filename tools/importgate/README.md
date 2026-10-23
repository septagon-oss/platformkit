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

For a measurement without a policy verdict:

```sh
go run ./tools/importgate --edges /path/to/product github.com/septagon-oss/platformkit
```

This writes `platformkit.import-edges.v1` JSON to stdout: sorted owner pairs, each
holding unique direct package edges and their sorted source contexts. Allowed and
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
