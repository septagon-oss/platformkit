# Review public compatibility

One tool, `scripts/check_public_api.py`, answers two questions about the exported surface
of `github.com/septagon-oss/platformkit`: whether a change broke the release line
(`make check-apidiff`, run by `make check`), and what one published revision owes another
(the release review). [docs/COMPATIBILITY.md](../docs/COMPATIBILITY.md) owns the rule both
answers are measured against.

## Keep the line: `make check-apidiff`

```sh
make check-apidiff
# python3 scripts/check_public_api.py --line-record scripts/baselines/public-api-v2.0.0-candidate.json HEAD
```

The line record holds three fields, each one read: `line`, the tag that is the base once
the owner publishes it, `frozen.commit`, the revision the surface froze at while that tag
does not exist, and `note`, the sentence a person finds when they open the file. The base
is resolved at its `git merge-base` with the candidate, so `main` is judged against the
frozen surface and a branch cut before the freeze is judged on its own delta; the run
prints which of the two it judged.

**Any reported incompatibility refuses the run.** No list of accepted changes is passed
and none is read, so a break somebody wrote down is refused exactly like one nobody did:

```
line v2.0.0: exported surface frozen at 45fbddf52d84 (judged by the frozen surface)
BREAK ./kit/httpx.(*API).Static: removed
BREAK ./modules/auth/contracts.Service.Sessions: added
```

One `BREAK ` line per reported sentence, that sentence verbatim, `./package.Name` written
module-relative as the pinned tool writes it in `-m` mode. Moving the freeze is the
owner's, in a commit of its own whose one-line diff is the review; a red gate is not a
reason.

## Review a published line: two named revisions

```sh
python3 scripts/check_public_api.py v1.1.0 HEAD > /tmp/platformkit-public-api.json
```

This is the comparison a release review runs against a tag, and the scheduled
[public-consumption workflow](../.gitea/workflows/public-consumption.yml) runs it weekly.
Its verdict is about the published version rather than about the diff, so any reported line
is a finding to resolve rather than a gate on a change: `result` says `review required`,
and the exit code is 1.

## Both shapes

The script reads committed Git objects, including default HEAD, and excludes working
edits. It needs Python 3.12+, Go and access to public dependencies. It selects the source
go.mod toolchain, runs pinned official apidiff in automatically removed source exports, and
records both commits and the exported-type report. It starts no services; dependency
downloads use the ordinary Go cache.

Exit 0 means nothing was reported, 1 means the report holds an incompatibility, and 2 means
execution failed — an unreachable proxy after both waits, a revision that does not resolve,
or a line record naming neither a published tag nor a frozen commit. A run that could not
happen never reports success. Retain JSON evidence with the release review.

The report excludes internal packages and does not establish behavioural compatibility. It
sees no JSON struct tag, no served wire field and no CLI flag, and it compares the shape of
no name absent at the compared revision; docs/COMPATIBILITY.md names what refuses each of
those. See the [official tool contract](https://pkg.go.dev/golang.org/x/exp/apidiff) for
its approximation and omissions.

## What this repository used to do instead

`--baseline` and `--write-baseline` compared the report against a reviewed list of
incompatibilities the line already owed, and `scripts/baselines/public-api-v1.1.0.json`
carried 116 of them. Both are gone, because a list licenses what it names: measured against
a frozen revision, a baseline carrying `./library.Open: removed` turned that removal into a
pass. A line that accumulates breaks nobody intends to revert is a line whose next release
is a major, which is the decision the retired file was standing in for. Do not re-add the
flag: the gate's whole claim is that a documented break and an accidental one refuse alike.
