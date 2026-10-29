#!/usr/bin/env bash
# Every claim the composition-citation case reads must be said once in the map it reads.
#
# The case in kit/fault that holds ARCHITECTURE.md's composition citations to the code they
# name finds each claim at its first occurrence, because that is where a reader meets the
# citation and the document wraps its sentences, so the case matches a claim word by word
# over any run of whitespace. A claim said twice — a sentence copied into a summary while
# the section that wrote it keeps its own — therefore aims that case at whichever paragraph
# comes first. Where the first paragraph cites no path the case stops with a complaint about
# a claim nobody moved; where it cites the path, the case reads the summary, passes, and the
# citation that stayed behind in the section is unchecked for as long as the copy stands.
#
# So the precondition of that case is the document's: one claim, one sentence, one citation.
# The claims are read out of the case's own row table, so a claim added there is covered
# without editing this file, and a table that cannot be read is refused rather than passed
# on as nothing. The repetition is paired with the map as it stands, so a green line here
# means the check saw the copy and not an unrelated complaint about the fixture.
set -uo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
case_file="$root/kit/fault/architecture_composition_citations_name_the_call_their_sentence_names_test.go"
map_doc="$root/ARCHITECTURE.md"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fails=0

for input in "$case_file" "$map_doc"; do
	[ -f "$input" ] || {
		echo "claim-said-once: $input is not here, so the case this one asks asked nothing" >&2
		exit 2
	}
done

# said_once.py counts one document against one row table: exit 0 when every claim the table
# names is said once, exit 1 naming each claim said any other number of times, exit 2 when
# the table holds no claim to count.
cat >"$work/said_once.py" <<'PY'
import re
import sys

doc = open(sys.argv[1], encoding="utf-8", errors="replace").read()
table = open(sys.argv[2], encoding="utf-8", errors="replace").read()
# One row per claim: `{"<claim>", "<path>", "<code>"},` — the case's own table.
claims = re.findall(r'^\t\t\{"((?:[^"\\]|\\.)*)", "', table, re.M)
if not claims:
    print("no claim row could be read from the table", file=sys.stderr)
    sys.exit(2)
wrong = 0
for claim in claims:
    occurrences = len(re.compile(r"\s+".join(map(re.escape, claim.split()))).findall(doc))
    if occurrences != 1:
        print(f"{claim!r} is said {occurrences} time(s) in the map", file=sys.stderr)
        wrong += 1
sys.exit(1 if wrong else 0)
PY

verify() { # $1 = map document, $2 = row table, $3 = expected exit, $4 = label
	local status out
	out=$(python3 "$work/said_once.py" "$1" "$2" 2>&1)
	status=$?
	if [ "$status" != "$3" ]; then
		echo "FAIL  $4: exit $status, want $3"
		printf '%s\n' "$out" | sed 's/^/      /'
		fails=$((fails + 1))
		return
	fi
	echo "ok    $4"
}

verify "$map_doc" "$case_file" 0 "the map as it stands says each of the case's claims once"

# The repetition a later editing round causes: the citation's own sentence, written a second
# time in a summary that sits above the section that wrote it. The case reads the summary,
# finds the line the summary quotes correctly, and never reaches the section's own sentence —
# so a citation that rots there rots unread.
{
	printf '%s\n\n' 'What the shell is handed is the same pair the composition hands the shell at `apps/platformkit/app.go:317`.'
	cat "$map_doc"
} >"$work/summary-first.md"
verify "$work/summary-first.md" "$case_file" 1 "the same sentence copied into a summary above the section that wrote it"

# And the vacuity the case would otherwise keep: a table with no claim row to read. The
# answer is not "passed", it is "this asked nothing".
printf 'package fault_test\n' >"$work/no_rows_test.go"
verify "$map_doc" "$work/no_rows_test.go" 2 "a row table with no claim in it"

if [ "$fails" -gt 0 ]; then
	echo "claim-said-once: $fails case(s) failed"
	exit 1
fi
echo "claim-said-once: each claim the composition-citation case reads is said once, so it reads the sentence that cited"
