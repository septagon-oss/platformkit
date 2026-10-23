# Keep the release line

A **release line** is the tag `vMAJOR.MINOR.0` — `v1.1.0`, `v2.0.0` — with the patch
releases that follow it. Within a line the exported surface only grows or gains
deprecations: an exported Go identifier, a wire field, a JSON tag, a catalogue key and a
CLI flag are **only ever added or deprecated**, and removal and reshaping happen only in
a major. A deprecation stays at least one minor before the major that removes it.

This document owns that rule. [RELEASE.md](../RELEASE.md) owns the procedure that
publishes a version, and [the pinned comparison](../scripts/PUBLIC-API.md) owns how a
comparison is run; neither restates what is written here.

## What may change, and what refuses it

The five surfaces above are the promise. Each row says which mechanism refuses a change
to it at this revision — and where nothing refuses one, it says so rather than implying
a gate, because a promise nobody checks is a review rule and says best when it is named
as one.

| Surface | Refused by |
| --- | --- |
| exported func, type, method, const, var, struct field, interface method | `make check-apidiff`, which reports `removed` and `changed from …` |
| the value of an exported constant — a catalogue key or a flag default, where it is an exported const | `make check-apidiff`, which reports `value changed from …` |
| a method **added to an exported interface** | `make check-apidiff`, which reports it as `added` — and it is a break, because every implementation a consumer wrote stops compiling |
| a JSON struct tag on an exported field | nothing yet: measured, a tag appearing or changing on an exported field reports no line; the reviewer reads the golden-document diff |
| a wire field of the served OpenAPI document | `check-generated` refuses a golden document nobody regenerated, not a field that disappeared; the diff of `apps/platformkit/testdata/openapi.json` is the review evidence |
| a CLI flag of `apps/platformkit` | nothing yet; the browser journey passes `--config`, so that one flag is exercised |
| a published journey step (`e2e/steps/kernel.ts`, `e2e/steps/public.ts`), which the phone compiles against | `make check-step-library` |

An exported interface is not a surface that "only ever grows": a method added to one
breaks every type that implements it, so the sentence above does not apply to it. This is
the most common shape of change in this repository, so the gate refuses it and the
contributor adds the capability beside the interface — a second interface, an option, a
free function — instead of widening one.

**Compatible by construction** is how a change lands on a line: the new thing is added
beside the old one, the old one delegates to it, and the old one is marked
`// Deprecated:` with its replacement named. Removing the old one is a later step, in a
major, listed where the next major reads ([the next major](#before-the-next-major)).

## Deprecation

`// Deprecated:` names a replacement that works, in the same sentence, and the deprecated
form keeps working through the supported line. An alias repairs a *removal*; it does not
repair a *move*, because the comparison resolves the alias and still reports the change —
that is how the 29 moved types in v1.1.0 came to be unrepairable
([CHANGELOG.md](../CHANGELOG.md)).

Nothing in this repository reads a `// Deprecated:` comment, so the one-minor window is a
review rule, kept honest by [docs/compatibility/](compatibility/): a removal planned for
the next major is written there, and a reviewer reads that list before approving the
removal. Writing a name in that list authorises nothing on the current line — see
[the next major](#before-the-next-major).

## The gate

```sh
make check-apidiff
# python3 scripts/check_public_api.py --line-record scripts/baselines/public-api-v2.0.0-candidate.json HEAD
```

The line record names the revision the surface is frozen at:

```json
{ "note": "…", "line": "v2.0.0", "frozen": { "commit": "45fbddf52d84…" } }
```

`line` is the tag that becomes the base the moment the owner publishes it; `frozen` is the
base while no such tag exists. The base is `git merge-base` of that revision and the
candidate, so main is judged against the frozen surface and a branch cut before the freeze
is judged on its own delta rather than refused for a change it never made. The run prints
which of the two it judged:

```
line v2.0.0: exported surface frozen at 45fbddf52d84 (judged by the frozen surface)
BREAK ./kit/httpx.(*API).Static: removed
```

One `BREAK ` line per reported incompatibility, the comparison's own sentence verbatim
after the prefix, and the same report in the evidence JSON on stdout. **Any reported line
refuses the run**: there is no list of accepted changes to add a name to, so a break
somebody wrote down is refused exactly like one nobody did. Exit 0 is a clean surface, 1 a
reported change, 2 a run that could not be made — an unreachable proxy after both waits, a
revision that does not resolve, a record naming neither a tag nor a frozen commit. A
refusal about the host never becomes a pass, and a pass never rests on a run that failed.

The gate sits in `make check`, so in `check-merge` and the forge's check job, and not in
`make check-push`: it needs the module proxy and costs about four minutes. That is where
an incompatible change is refused — before it reaches `main`'s consumers, not on push.

## Moving the freeze

Only the owner moves a freeze: edits `line` in the record, moves `frozen.commit`, or cuts
the tag. It is its own commit, `build(compat): …`, whose one-line diff *is* the review, in
the manner a budget ceiling change is committed alone. A red gate is not a reason to move
a freeze: the cure for a break that landed on `main` is the revert or the owner's freeze
move, never a re-measure, and a contributor may not do either.

## Before the next major

`docs/compatibility/<next-major>.md` — [v3.0.0.md](compatibility/v3.0.0.md) today — lists
the removals the next major owes and authorises nothing on the current line. Cutting a tag
is the owner's, and whether the next major lands on this module path or on a `/v2` one is
decided in [RELEASE.md](../RELEASE.md#choose-the-compatible-release-line), which leaves the
question open.

## Proof tags

`v1.1.2-proof.<yyyymmdd>.<sha>` tags are prereleases: a build of `main` published so that a
pinned consumer can compile against it. A proof tag carries **no compatibility claim** —
pseudoversions and prereleases establish none, as RELEASE.md already says — and they order
by the date in their identifier, not by ancestry, so a tag from a later merge can sort
before one from an earlier merge on the same day. A proof tag is never a release line and
never the base of this comparison.

## Consumers as evidence

A release line is a promise to somebody who compiles against it, so a tag waits for the two
checks that a consumer runs, not for a local run. The phone's `kernel-main.yml` fetches
the catalogue and the served API document at `main`'s own tip and fails when the answer
is no; the clients' `kernel-main.yml` moves its platformkit pin to kernel main inside its
own job, builds every composition and gates it, and never commits. Both answer the
question about the kernel that exists tonight, which is the question a line answers for
years, and the owner cuts the tag once that evidence is green. Nothing here cuts a tag.
