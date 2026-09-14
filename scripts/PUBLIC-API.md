# Review public compatibility

The release owner compares the supported stable tag with the exact candidate
commit before choosing a version. From the foundation repository:

```sh
python3 scripts/check_public_api.py v1.0.0 HEAD > /tmp/platformkit-public-api.json
```

This reads committed Git objects, including the default HEAD; it does not inspect
uncommitted changes. It needs Python 3.12+, Go and access to the selected module's
public dependencies. It selects the Go version from the source go.mod, runs the
pinned official apidiff tool in automatically removed source exports, and records
both resolved commits and the exported-type report. It starts no services and
publishes nothing. Dependency downloads use the ordinary Go cache.

Exit 0 means no incompatibilities were reported, 1 means review is required, and
2 means execution failed. Neither result proves behavioral compatibility.
Internal packages are excluded. Review alias/type-identity reports explicitly;
verify examples and actual consumers, public wire behavior and migration paths.
The [official tool contract](https://pkg.go.dev/golang.org/x/exp/apidiff) explains
its approximation and omissions.

Current main has known API incompatibilities with v1.0.0. Keep this as an explicit
release-preparation check until the baseline and major-version migration are
resolved; do not add it to make check and then suppress the expected report.
[RELEASE](../RELEASE.md) owns publication and migration requirements. This report
does not authorize a tag, release, environment change or deployment.
