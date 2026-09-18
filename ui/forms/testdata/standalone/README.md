# Independent package consumer

The two-editor example verifies public entity, forms, locale/xtext and Task
domain consumption through a small caller-owned controller. Its tests exercise
localized captured controls and the existing pure decision. This is a temporary
composition fixture, not an adopted client product.

From the foundation root, verify one exact published version:

```sh
python3 scripts/check_public_module.py v1.1.0 > /tmp/platformkit-public-module.json
```

That version is a published tag, so the check above is the proof an outside
consumer gets: one `go get` of a real version, no workspace, no `replace`, no
branch. Pin an exact version rather than a range — v1.1.0 is not source-compatible
with v1.0.0, by decision and not by accident, and [CHANGELOG](../../../../CHANGELOG.md#110---2026-09-18)
says what moved and why. The `/v2` module path and import migration in
[RELEASE](../../../../RELEASE.md#choose-the-compatible-release-line) remain owed to
any consumer outside this organisation. When the version changes, change it here;
the weekly
[public-consumption workflow](../../../../.gitea/workflows/public-consumption.yml)
reads this line, fails if the version it names does not resolve through the
public proxy, and fails if it is not a commit of this repository.

The script needs Python 3, Go and public proxy/checksum-service access. It selects
the source [go.mod](../../../../go.mod) toolchain, uses fresh external caches and
downloads through proxy.golang.org and sum.golang.org. It checks ordinary require
selection without workspace or replace fallbacks, then compiles, tests and vets
the example. It removes its fixture and caches without running the example's
server or starting services. Network/tool failures fail the check. Retain its
JSON source hashes, selected module, checksums, commands and runtime dependencies
as an exact CI artifact.

Alongside [package dependency rules](../../../../scripts/check_packages.sh), this
check rejects the application runtime, `database/sql` and broker dependencies.
[CONTRIBUTING](../../../../CONTRIBUTING.md) owns verification at product boundaries.
