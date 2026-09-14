# Independent package consumer

This is the existing two-editor example used to verify public entity, forms,
locale/xtext and Task domain consumption. It uses ordinary Go imports and a
small caller-owned controller; there is no PlatformKit application, SQL or broker.
Its tests exercise localized captured controls and the existing pure decision.
The example is a temporary composition fixture, not an adopted client product.

From the foundation root, verify one exact published version:

```sh
python3 scripts/check_public_module.py v1.0.1-0.20260913210353-40c63af55968 > /tmp/platformkit-public-module.json
```

The script needs Python 3, Go and public proxy/checksum-service access. It selects
the Go version in the source [go.mod](../../../../go.mod), uses fresh external caches, downloads the
specified version through proxy.golang.org and sum.golang.org, and checks ordinary
require selection without workspace or replace fallbacks. It compiles the example,
runs its existing tests and vet, then removes its fixture and caches. It never
runs the example's server or starts supporting services. Network/tool failures
fail the check. JSON output records the source-example hashes, selected module,
checksums, commands and runtime dependencies; retain it as an exact CI artifact.

Package dependency rules remain in [check_packages.sh](../../../../scripts/check_packages.sh). This consumer's
additional check refuses application/runtime dependencies in the assembled
example. Passing it does not establish browser behavior, database durability,
release compatibility, deployment health or product adoption. Use the package
and product checks documented in [CONTRIBUTING](../../../../CONTRIBUTING.md) for those boundaries.
