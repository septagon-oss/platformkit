#!/usr/bin/env bash
# The composition-citation guard must reach every row the case asserts, not only the rows
# typeset in the shape the guard happens to read.
#
# The guard next to this one takes its claims out of the case's own row table with a pattern
# anchored on how a row is written today: two tabs, `{"`, the claim, `", "`. That is what lets
# a claim added to the table be covered without editing the guard. It is also a second copy of
# the table's shape: a row a formatter or an editor reflows onto two lines, or a claim lifted
# into a named constant, leaves the guard's reach while the case still asserts it, and the
# guard then answers "every claim said once" about a shorter list than the one it was handed.
# Losing every claim is refused, because an empty table exits 2; losing some of them is the
# quiet shape, and this is what refuses it.
#
# The rows are counted a second way here, by tokenising the table's own string and brace
# tokens so the count does not depend on where a row's line begins or ends. A table this file
# cannot tokenise exits 2 rather than passing on nothing, and a claim out of the guard's reach
# is named by its own text.
set -uo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
case_file="$root/kit/fault/architecture_composition_citations_name_the_call_their_sentence_names_test.go"

[ -f "$case_file" ] || {
	echo "claim-coverage: $case_file is not here, so the table this one counts asked nothing" >&2
	exit 2
}

python3 -B - "$case_file" <<'PY'
import re
import sys

path = sys.argv[1]
text = open(path, encoding="utf-8", errors="replace").read()

head = text.find("range []struct{")
if head < 0:
    print(f"claim-coverage: no `range []struct{{ … }}{{ … }}` table in {path}", file=sys.stderr)
    sys.exit(2)

# Tokenise the Go from there: quoted text is read whole, so a brace inside a claim or inside
# the code a row names (a row that cites `type Value struct {`, say) cannot open a row.
events = []
i = head
while i < len(text):
    ch = text[i]
    if ch == '"':
        j = i + 1
        while j < len(text):
            if text[j] == "\\":
                j += 2
                continue
            if text[j] == '"':
                break
            j += 1
        events.append(("string", text[i + 1:j]))
        i = j + 1
        continue
    if ch == "`":
        j = text.find("`", i + 1)
        if j < 0:
            break
        events.append(("string", text[i + 1:j]))
        i = j + 1
        continue
    if ch in "{}":
        events.append((ch, None))
    i += 1

# The first brace pair after `range []struct` is the struct type, the second opens the table.
opens = [n for n, (kind, _) in enumerate(events) if kind == "{"]
if len(opens) < 2:
    print(f"claim-coverage: the table in {path} never opens", file=sys.stderr)
    sys.exit(2)

claims = []
depth = 0
started = False
for kind, value in events[opens[1] + 1:]:
    if kind == "{":
        depth += 1
        started = depth == 1
        continue
    if kind == "}":
        depth -= 1
        if depth < 0:
            break  # the table is closed; whatever follows belongs to the case body
        continue
    if started and depth == 1 and kind == "string":
        claims.append(value)
        started = False
if not claims:
    print(f"claim-coverage: the table in {path} holds no row to count", file=sys.stderr)
    sys.exit(2)

# The guard's own extraction, copied from its Python rather than re-derived, so this refuses
# when guard and table drift apart rather than when both move together.
seen = re.findall(r'^\t\t\{"((?:[^"\\]|\\.)*)", "', text, re.M)

missing = [claim for claim in claims if claim not in seen]
extra = [claim for claim in seen if claim not in claims]
if missing or extra:
    print("claim-coverage: the guard reads %d claim(s) of the table's %d" % (len(seen), len(claims)))
    for claim in missing:
        print(f"   named by a row, out of the guard's reach: {claim!r}")
    for claim in extra:
        print(f"   read by the guard, named by no row: {claim!r}")
    sys.exit(1)
print(f"claim-coverage: the guard reads one claim for every row of its table ({len(claims)})")
PY
status=$?
[ "$status" = 0 ] || exit "$status"

# The guard is worth this much care only because something runs it. `make check` reaches it
# through the implementation map's own goal; the day it is dropped from that recipe, every
# claim it holds goes unchecked without a single check turning red.
if ! grep -q 'a_claim_the_composition_case_reads_is_said_once_in_the_implementation_map_test\.sh' "$root/Makefile"; then
	echo "claim-coverage: no make goal runs the guard whose claims this one counts"
	exit 1
fi
echo "claim-coverage: a make goal runs that guard"
