#!/usr/bin/env bash
# No comment in a kernel test cites the round that asked for it; it cites the behaviour.
#
# The rename that gave a kernel test its sentence (047f624, "every test file is named for what it
# protects") moved the round number out of the file name and left it in the paragraph underneath,
# where it asks the reader for a document that is not in this repository: to learn what "Review
# round 20 (T-0110)" defends, a person has to find the review, and to find the review they have to
# know which task this was. Every case whose opener began that way states the same thing in its next
# sentence, which is why dropping the number costs nothing — and where a mid-comment citation names
# another case's finding rather than a behaviour, the fix names the file that holds it.
#
# Three properties make this refusal more than a grep.
#
#   * It reads a comment *group*, with its line breaks collapsed. The opener of
#     kit/db/the_run_puts_the_budgets_back_the_way_it_found_them_test.go narrated "is review" at the
#     end of one `//` line and "round 14's pin" at the start of the next, and a line-at-a-time match —
#     `rg`, and this repository's own per-package comment pins — passes straight over it. That blind
#     spot is why the planted cases below exist: a matcher that never fires on the shape it names is
#     green by accident.
#   * It reads every comment line of a test file, not its first block. A citation that survives in a
#     helper's doc comment or beside a fixture sends the reader to the same missing document.
#   * It applies to every test file whose own name names no round. The files whose names do are the
#     ones the loop's naming census counts as debt, at the ceiling that gate holds, and their prose is
#     held with their names. The exclusion is derived from the file's name rather than listed, because
#     a list would go on excluding a file whose name had already lost its round — the exact rename this
#     branch exists to perform.
#
# The domain word "review" is not in the pattern, on purpose: modules/change, modules/billing and the
# approval journeys review a *proposal*, and a guard that refused that word would be refusing the
# product's own language. What is refused is a round, a finding number and an attempt number — the
# three ways these comments have of citing a discussion instead of a behaviour.
#
# The matcher is deliberately blunt about the round, and its one known cost is arithmetic. Because it
# matches `rounds?\s+\d+` to catch the plural a citation takes when two rounds are named together, a
# comment that *rounds* a number is refused too: "A worker rounds 5.2 to 5" reads as a citation of
# "rounds 5" to it. Narrowing that — dropping the `s`, or refusing a bare integer so a decimal stays
# legal — buys the arithmetic line back and loses the citation it was added for, so the blunt shape
# stays and the trade is written down rather than discovered by whoever writes the arithmetic. The
# planted case below pins the refusal as today's boundary, so a future narrowing fails there and has
# to be argued with that case, not found by surprise. An arithmetic comment that needs to pass today
# names its operands without the verb: "5.2 to 5", "half rounded up".
set -euo pipefail

cd "$(cd "$(dirname "$0")/.." && pwd)"

# The planted files live outside the tree, in a directory of this run's own, so nothing here is a
# file a later build could pick up and nothing here is in the tree the scanner reads.
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
mkdir -p "$scratch/pinned"
cat >"$scratch/pinned/wrapped_test.go" <<'GO'
package pinned

// the_case_that_waits_test.go is review
// round 14's pin over the commit that drew the line.

func TestTheCaseThatWaits(t *testing.T) {}
GO
cat >"$scratch/pinned/mid_file_test.go" <<'GO'
package pinned

func TestTheLedgerGrows(t *testing.T) {}

// deepHelper reads the ledger. Round 8 measured the number this helper defends.
func deepHelper() int { return 0 }
GO
cat >"$scratch/pinned/arithmetic_test.go" <<'GO'
package pinned

// A worker rounds 5.2 to 5 and never the other way.

func TestAWorkerRounds(t *testing.T) {}
GO
cat >"$scratch/pinned/clean_test.go" <<'GO'
package pinned

// The session keeps the budgets it was handed, and this case reads them from inside the run.

func TestTheSessionKeepsItsBudgets(t *testing.T) {}

// deepClean reads the ledger after one round trip through the propagator and reports what the
// tenant-attributed series holds. Nothing here cites a discussion.
func deepClean() int { return 0 }
GO

python3 - "$scratch/pinned" <<'PY'
import re, subprocess, sys

TEST_SUFFIXES = ("_test.go", "_test.sh", "_test.py", ".spec.ts", ".test.ts", ".test.tsx")
# The same shape the loop's census gate counts, so the two agree on which files are held as debt.
CENSUS = re.compile(r"(^|/)[^/]*(review|probe|round[0-9])[^/]*(_test\.go|\.test\.tsx?|\.spec\.ts|_test\.py|_test\.sh)$")
CENSUS_CEILING = 22
# A round, a finding's number, an attempt's number. "round trip", "rounded 5" and "background 5"
# form none of these: the number has to stand where the round's ordinal stands. The `s?` that lets
# "rounds 8 and 9" be caught is also what refuses "rounds 5.2 to 5"; see the header for that trade
# and for the planted case that holds it.
NARRATION = re.compile(r"(?i)\b(review[\s-]+rounds?\b|rounds?\s+\d+|findings?\s+#?\d+|attempt\s+\d+)")


def comment_runs(text):
    """Every run of whole-line comments, as (first line, text with line breaks collapsed)."""
    runs, run, start = [], [], 0
    for n, line in enumerate(text.split("\n"), 1):
        stripped = line.lstrip()
        body = None
        if stripped.startswith("//"):
            body = stripped[2:].strip()
        elif stripped.startswith("#") and not stripped.startswith("#!"):
            body = stripped[1:].strip()
        if body is not None:
            if not run:
                start = n
            run.append(body)
        elif run:
            runs.append((start, " ".join(run)))
            run = []
    if run:
        runs.append((start, " ".join(run)))
    return runs


def scan(paths, read):
    bad, checked = [], 0
    for path in paths:
        checked += 1
        for line, group in comment_runs(read(path)):
            hit = NARRATION.search(group)
            if hit:
                bad.append(f"{path}:{line}: a comment names {hit.group(0)!r}")
    return checked, bad

tracked = [p for p in subprocess.run(["git", "ls-files"], capture_output=True, text=True).stdout.split("\n")
           if p and p.endswith(TEST_SUFFIXES)]
# This file is the one path the scan exempts, and it is exempted by name in the open. Its prose quotes
# the shape it refuses — "Review round 20 (T-0110)" in its own explanation, and the planted phrases it
# writes to disk below — so it would report itself, and the case underneath proves that rather than
# asking anyone to take it on trust. The alternative, spelling the planted phrases by concatenation so
# they cannot match, costs a reader the very phrase the file exists to show; kit/appname/census_test.go
# refuses that trade for the same reason and names its own file the same way.
SELF = "scripts/test_comments_explain_what_they_protect_test.sh"
held = sorted(p for p in tracked if CENSUS.search(p))
living = [p for p in tracked if p not in held and p != SELF]
self_prose = scan([SELF], lambda p: open(p, encoding="utf-8").read())[1] if SELF in tracked else []
checked, bad = scan(living, lambda p: open(p, encoding="utf-8").read())
print(f"ok   {checked} test files whose own name names no round carry no comment naming one")
if SELF not in tracked:
    bad.append(f"{SELF} is untracked, so the scan that runs on git ls-files reads nothing it guards")
if len(self_prose) < 3:
    bad.append(f"{SELF} reports {len(self_prose)} refusals against its own prose; the exemption below "
               "is only honest if this file really does contain the shapes it refuses")
else:
    print(f"ok   this file would be refused {len(self_prose)} times by its own rule, and is exempt by name")
if len(held) > CENSUS_CEILING:
    bad.append(f"the exclusion by file name covers {len(held)} files, over the {CENSUS_CEILING} the census gate holds")
print(f"ok   {len(held)} files are held by their own names, at or under the census gate's {CENSUS_CEILING}")

# The planted trio: the wrapped phrase and the mid-file citation must be caught, and the clean file —
# which uses the domain term "round trip" — must not. These are the matcher's own assertions, run on
# bytes no rename will ever tidy.
planted = sys.argv[1]
_, caught = scan([f"{planted}/wrapped_test.go"], lambda p: open(p, encoding="utf-8").read())
_, middle = scan([f"{planted}/mid_file_test.go"], lambda p: open(p, encoding="utf-8").read())
_, quiet = scan([f"{planted}/clean_test.go"], lambda p: open(p, encoding="utf-8").read())
_, arith = scan([f"{planted}/arithmetic_test.go"], lambda p: open(p, encoding="utf-8").read())
if len(caught) != 1 or "review round" not in caught[0]:
    bad.append("the planted phrase wrapped across two // lines was not caught as the round it is: "
               f"{caught} — a comment group broken across lines escapes the match")
else:
    print("ok   a phrase wrapped across two comment lines is caught, and named as the round it is")
if len(middle) != 1 or "round 8" not in middle[0].lower():
    bad.append(f"a citation in a helper's doc comment was not caught: {middle}")
else:
    print("ok   a citation below the file's opening block is caught")
if quiet:
    bad.append("a comment that names behaviour, or the domain term 'round trip', was refused: " + "; ".join(quiet))
else:
    print("ok   a comment that names behaviour, and the domain term 'round trip', are not refused")
# The known limit, written down: an arithmetic sentence is refused today, and this is the case that
# says so. A matcher narrowed to let it through fails here, where the trade is written, rather than
# quietly losing the plural citation the `s?` was there for.
if len(arith) != 1 or "rounds 5" not in arith[0]:
    bad.append(f"the planted arithmetic comment was reported as {arith}; the matcher was either "
               "narrowed — then update this case and the header paragraph together — or broken")
else:
    print("ok   an arithmetic 'rounds 5.2 to 5' is refused today, and the case that says so fails if "
          "the matcher narrows")

for line in bad:
    print("REFUSAL " + line)
if bad:
    for path in held:
        print("held by name: " + path)
sys.exit(1 if bad else 0)
PY

# A guard no recipe invokes is the same mistake as a case no reader can run.
if grep -q 'bash scripts/test_comments_explain_what_they_protect_test.sh' Makefile; then
	echo "ok   make check runs scripts/test_comments_explain_what_they_protect_test.sh"
else
	echo "REFUSAL Makefile's check recipe does not run this file, so nothing runs it" >&2
	exit 1
fi

echo "test comments: no comment in a kernel test cites the round that asked for it"
