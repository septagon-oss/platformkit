# Independent package consumer

The two-editor example verifies public entity, forms, locale/xtext and Task
domain consumption through a small caller-owned controller. Its tests exercise
localized captured controls and the existing pure decision. This is a temporary
composition fixture, not an adopted client product.

From the foundation root, verify one exact published version:

```sh
python3 scripts/check_public_module.py v1.0.1-0.20260917134124-d3f81bdc2cab > /tmp/platformkit-public-module.json
```

That version is the tip of `main` at the time of writing, not a release: the
`/v2` line in [RELEASE](../../../../RELEASE.md#choose-the-compatible-release-line)
is still open, so an outside consumer takes a pseudo-version the way the
commercial catalog and a client application do. When the version changes, change
it here; the weekly
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
