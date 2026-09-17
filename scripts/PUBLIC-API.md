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

## Compare against the accepted line

Where a release line already carries breaking changes nobody intends to revert,
measure the delta instead of the pile:

```sh
python3 scripts/check_public_api.py v1.0.0 HEAD --baseline scripts/PUBLIC-API-BASELINE.json
python3 scripts/check_public_api.py v1.0.0 HEAD --baseline <path> --write-baseline   # re-record
```

The baseline is a reviewed list of the changes already accepted on this line. It
fails on a reported line the file does not name, and on a named line the report no
longer produces — a baseline that cannot go stale is a list of excuses, which is
the same rule `scripts/check_budget_ratchet.sh` applies to a removed bucket. The
measured report is written into the same evidence JSON beside the delta, so the
baseline narrows what fails the step and hides nothing: with it, this comparison is
a gate, and [.gitea/workflows/public-consumption.yml](../.gitea/workflows/public-consumption.yml)
runs it weekly. Re-recording is `--write-baseline`, and its diff is the review: run
it deliberately and commit it alone, the way a ceiling change is committed alone.

Keep the plain comparison out of `make check`: it resolves published revisions, so
it needs the network and a supported tag, and neither belongs in a gate that must
run offline. What this does not do is close the [release decision](../RELEASE.md#choose-the-compatible-release-line)
the baseline is standing in for: publishing a stable line ends the file. Do not
suppress the expected report to make a step pass.
