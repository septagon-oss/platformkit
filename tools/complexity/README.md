# Complexity ceilings

`go run ./tools/complexity` reports seven source-debt counts without reading a
budget. `make check-complexity` requires each count to equal its ceiling in
[complexity-budget.json](../../complexity-budget.json) and validates every budget
transition since the merge base with `origin/main`. Both `make check` and
`make check-push` include this gate.

```sh
go run ./tools/complexity --root .
make check-complexity COMPLEXITY_BASE=origin/main
go run ./tools/complexity --check --base FULL_ANCESTOR_COMMIT
go run ./tools/complexity --history-only --merge-base origin/main
```

`--root` resolves to the containing Git worktree. Checking requires exactly one
of `--base` (a full ancestor SHA) and `--merge-base` (a reference resolved once).
`--check` and `--history-only` are mutually exclusive. The latter validates an
allocation before its source is implemented; Make always uses the full check.
There is no write, rebaseline, fetch, or threshold flag. Compiled statuses are
0 for success, 1 for policy refusal, and 2 for invalid input or operational
failure. `go run` collapses nonzero statuses; either still fails Make.

## Measurements

| Key | Counts |
| --- | --- |
| `funcs_over_100` | Body-bearing function/method declarations spanning more than 100 physical lines, inclusive. Nested literals belong to their enclosing function. |
| `cc_over_30` | Functions with complexity above 30. Start at 1; add 1 per if, for, range, non-default case, non-default communication clause, and `&&` or `||`, including nested literals. A multi-value case adds one. |
| `files_over_800` | Files with more than 800 newline bytes; an unterminated final line adds none. |
| `long_comment_blocks` | AST comment groups spanning at least 15 physical lines, including block comments and package documentation. |
| `history_comment_lines` | Comment-token lines matching the history pattern below or an ISO-shaped date, once per token line regardless of the number of matches. |
| `stutters` | Exported package-level types, values and body-bearing functions whose names start with the package name, ignoring case. Exempts methods, `main`, and package names of at most two bytes. |
| `httpx_exports` | Documentation declarations directly in `kit/httpx`: types, package functions (including constructors), and const/var documentation groups. Methods and fields are excluded; groups count once. The first displayed name must start with ASCII A–Z. |

The history pattern preserves the existing scoreboard's definition:

```text
(?i)(review ?\d|finding ?\d|round ?\d|T-\d{3,}|\bD-?\d{2,}\b|decision \d|ADR-?\d|r\d+ finding|codex|grok|deepseek)
```

It additionally matches `\b[0-9]{4}-[0-9]{2}-[0-9]{2}\b`, a written date shape,
not a calendar validator. Strings are not comments. Compatibility dates and
examples count too: this is an aggregate debt measure, not a semantic comment
linter. Distinct comment tokens on one physical line remain distinct inputs.
Positions ignore `//line` remapping and point to the matching block-comment line.

## Source boundary

The tool reads tracked and untracked non-ignored Go files, deduplicated and
sorted, including tracked ignored files and working edits to staged files.
Tests (`*_test.go`) and paths containing `vendor`, `node_modules`, `.git`, or
`testdata` are excluded. Non-test source in test support packages counts.
Build tags do not filter the AST. Parse/read errors refuse the run; selected
symlinks and nonregular files are refused. Deleted tracked files are absent. A Go
package in `kit/httpx` is required.

Git's effective `linguist-generated` attribute excludes values `set` and `true`;
`unset`, `false`, and `unspecified` include the file. Other values refuse the
run. Nested overrides apply. A generated header alone does not exclude a file.
Historical input comes directly from commit trees and `git check-attr --source`;
no historical code runs, no checkout is registered, and no ref or index changes.
Git must support that option. Local attribute overrides remain part of the
measurement environment; compare revisions with equivalent effective settings.

## Returning room and approving increases

A count above its ceiling prints its delta and **every** offender, ordered by
path, physical line, name, and size, with escaped control characters. A count
below its ceiling prints the exact value the author must write. Lower the budget
in the same source change. Equal counts pass, including zero. The JSON object
must contain exactly the seven integer keys above, without duplicates, metadata,
nulls, exponents, fractions, negatives, or values above the signed 64-bit maximum.

Every increase requires its own commit whose subject starts `build(complexity):`,
which changes **only** `complexity-budget.json` and carries a nonempty
`Owner-approved:` trailer parsed by Git. The checker verifies the recorded claim;
repository review authenticates the approval. An ordinary reduction needs no
approval. An uncommitted increase cannot borrow a prior approval. Intermediate
increases remain visible even when a later commit reverts them.

The checker visits every branch-exclusive commit once, in parent-before-child
order. At a merge, compare each parent with their unique common base. For each
measure, inherit the minimum among parent values different from that base; if
none changed, inherit the base. The merge may keep or lower that result, never
raise it. Thus common 10 and parents 12/10 inherit 12; 12/9 inherit 9; 12/14
inherit 12. A later isolated approved commit can price a higher value. Octopus
merges follow the same rule. Multiple common bases refuse with their SHAs.

If the trusted baseline has no budget, its measured counts become the virtual
initial ceiling. Introduction may share the feature commit and may only keep or
lower those values. A higher allocation needs a subsequent approved commit.
Pre-mechanism side histories inherit the trusted baseline values for comparison.
Deletion after introduction is always refused; it cannot restart initialization.
Malformed historical budgets refuse too. Shallow history hiding an inspected
parent edge is refused. Missing objects/refs never trigger a fetch (including
Git's lazy fetching) or fallback to HEAD; replacement objects are ignored.

CI supplies the pull request base or the push event's previous SHA through
`COMPLEXITY_BASE`, retaining full checkout history. Audits of already merged work
must explicitly supply an earlier base; the local default cannot inspect commits
already contained by `origin/main`. HEAD and the base are pinned and reported;
a changed HEAD refuses the run.

## Limits and initial census

This counts offenders, not their identities or severity. Replacing an offender
or enlarging an already oversized function can leave its count unchanged. Checker
and attribute changes require ordinary review. Working files are not a locked
snapshot: run on a stable tree, as CI does, and rerun after concurrent edits.

The initial census at `740a3e1944dc504fee3030a882b3e2f6143ab16e` is
98/66/25/580/113/24/108 in the table's order. The legacy scoreboard reports 100
history lines; the explicit date rule adds 13. Source selection and the HTTP
AST documentation count preserve the other six values on that revision. The
command's own source is included and introduces no additional offenders.
