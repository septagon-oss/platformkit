# Independent package consumer

The two-editor example verifies public entity, forms, locale/xtext and Task
domain consumption through a small caller-owned controller. Its tests exercise
localized captured controls and the existing pure decision. This is a temporary
composition fixture, not an adopted client product.

From the foundation root, verify one exact published version:

```sh
python3 scripts/check_public_module.py v1.0.1-0.20260913210353-40c63af55968 > /tmp/platformkit-public-module.json
```

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
