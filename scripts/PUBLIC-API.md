# Review public compatibility

Compare a supported stable tag with the exact candidate commit from the
foundation repository, following the [release prerequisites](../RELEASE.md#choose-the-compatible-release-line):

```sh
python3 scripts/check_public_api.py v1.0.0 HEAD > /tmp/platformkit-public-api.json
```

This reads committed Git objects, including default HEAD; it excludes working
edits. It needs Python 3.12+, Go and access to public dependencies. The script
selects the source go.mod toolchain, runs pinned official apidiff in automatically
removed source exports and records both commits and the exported-type report.
It starts no services; dependency downloads use the ordinary Go cache.

Exit 0 means no incompatibilities were reported, 1 requires review, and 2 means
execution failed. The report excludes internal packages and does not establish
behavioral compatibility. See the [official tool contract](https://pkg.go.dev/golang.org/x/exp/apidiff)
for its approximation and omissions. Retain JSON evidence with the release review.

Keep this separate from make check until the known v1.0.0 incompatibilities and
major-version baseline are resolved; do not suppress the expected report.
