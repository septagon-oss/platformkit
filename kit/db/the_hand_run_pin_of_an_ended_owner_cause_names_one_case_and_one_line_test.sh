#!/usr/bin/env bash
# The hand-run pin beside this package's cases holds three literals: the name of the Go case it
# runs, the block of the ending's log line it rewrites in its copy, and the two pieces of text it
# requires that case to print when it refuses. All three rot quietly — the case can be renamed, the
# log line reformatted, the assertion reworded — and the pin that carries them asks for a development
# PostgreSQL and a copy of the tree, so nobody learns until someone means to run it. Ask the same
# three questions of the source, in a second, with no database and no copy.
#
# Answers for the package as it stands; refuses, naming what moved, when the case the pin runs is
# gone, duplicated or no longer asks for the attribute, or when the block it guards is not there to
# guard. Writes nothing, needs no service.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
pin="$root/kit/db/a_confirmed_ending_that_drops_the_cause_its_caller_was_handed_is_caught_test.sh"
case_file="$root/kit/db/the_line_about_dropped_effects_of_an_ended_owner_names_the_cause_its_caller_was_handed_test.go"

[ -f "$pin" ] || { echo "FAIL: the hand-run pin is not at $pin"; exit 1; }
[ -f "$case_file" ] || { echo "FAIL: the case the pin runs is not at $case_file"; exit 1; }

python3 - "$pin" "$case_file" "$root/kit/db/tx.go" <<'PY'
import re
import sys

pin_path, case_path, tx_path = sys.argv[1:4]
pin = open(pin_path).read()
case = open(case_path).read()
tx = open(tx_path).read()

def refuse(what):
    print("FAIL: " + what)
    sys.exit(1)

# 1. The pattern the pin runs names exactly one case of this package. With none it would print
#    "no tests to run" and still read as a pass, so the pin's value rests on this name existing
#    once; with several it would judge a different case than the one it was written for.
patterns = re.findall(r"-run '([^']+)'", pin)
if len(patterns) != 1:
    refuse("the pin's -run pattern is not the one line this pin can read: %r" % (patterns,))
wanted = re.compile(patterns[0])
named = [m.group(1) for m in re.finditer(r"^func (Test[A-Za-z0-9_]*)\(", case, re.M)]
matched = [n for n in named if wanted.search(n)]
if len(matched) != 1:
    refuse("the pin's -run %s selects %d of this package's cases (%s), want one"
           % (patterns[0], len(matched), ", ".join(named) or "none declared in the file"))

# 2. The block the pin rewrites in its copy is there to rewrite: the unguarded append of the
#    caller's cause, exactly once in the ending's log line. The pin asks this of a copy it paid to
#    build; asking it of the source first turns a two-minute database run into a string compare.
guarded = '''	attrs := []any{"effects", dropped, "committed", e.committed, "rolled_back", e.rollbackConfirmed}
	if e.err != nil && !known {
		attrs = append(attrs, "error", e.err)
	}'''
plain = guarded.replace(' && !known', '')
if tx.count(plain) != 1:
    refuse("the block the pin guards is named %d time(s) in kit/db/tx.go, want once: the pin and the "
           "ending's log line have drifted and the pin's mutation no longer reaches it" % tx.count(plain))
if guarded in tx:
    refuse("kit/db/tx.go already carries the pin's mutated form, so the pin would rewrite nothing")

# 3. What the pin greps for in that case's refusal is the case's own words: the sentence it says
#    when an attribute is missing, and the two things it asks the line for — the attribute itself and
#    the owner's cause. If the case stops asking for either, the greps answer for a case that no
#    longer checks the thing the pin was written for.
if 'does not name %s' not in case:
    refuse("the case no longer says what it found missing in the line, so the pin's first grep reads "
           "a sentence nothing prints")
for want, why in (('"error="', "the attribute the cause travels in"),
                  ('refused.Error()', "the owner's cause the caller was handed")):
    if want not in case:
        refuse("the case no longer asks the line for %s (%s), which is what the pin exists to keep"
               % (want, why))
print("ok   the hand-run pin of an ended owner's cause names one case, one line and the words it greps")
PY
