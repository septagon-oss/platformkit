#!/usr/bin/env bash
# Nine answers of scripts/check_test_inventory.sh, pinned against a fake tree — and the exit status
# that carries each one (decision 0088).
#
# The checker reads the checkout and tests/inventory.json and never a database, so the fixture
# these cases need is a directory with two test files in it — the shape
# scripts/check_budget_ratchet_test.sh uses for the ratchet, and the reason a checker over tests
# needs no Postgres to prove what it refuses (decision 0088).
#
# Each case names the answer that decides it, and each one fails for a stated reason: an assertion
# here that could not fail is the same as the column that quoted nine wrong deletes. Review 1 found
# the shape of that mistake at a larger scale: the cases below greps of refusals piped out of the
# tool, which answered them however the tool exited, so a tool that printed every refusal and exited
# 0 kept all eight green. The status is now part of every case, and case 9 asks that a table the tree
# answers is what exits 0 — a refusal on every call would pass the eight above just as easily.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
checker="$root/scripts/test_inventory.py"
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT

fails=0
note() { printf 'FAIL: %s\n' "$*" >&2; fails=$((fails + 1)); }

mkdir -p "$fixture/tests" "$fixture/foo" "$fixture/bar"
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null
export GIT_AUTHOR_NAME=fixture GIT_AUTHOR_EMAIL=f@invalid GIT_COMMITTER_NAME=fixture GIT_COMMITTER_EMAIL=f@invalid
git -C "$fixture" init -q
# A go.mod, so `go list` can answer for this tree. The refusal cases stop at the push tier's stack
# promise, which the selector reaches before it consults the graph; case 9's green selection cannot,
# and a selector that refused every call would otherwise answer case 7 exactly as the honest one does.
module="$(sed -n 's/^module //p' "$root/go.mod")"
printf 'module %s\n\ngo %s\n' "$module" "$(sed -n 's/^go //p' "$root/go.mod")" > "$fixture/go.mod"
export GOWORK=off
cat > "$fixture/foo/foo_test.go" <<'GO'
package foo

import "testing"

func TestFoo(t *testing.T) {
	if Foo() != 1 {
		t.Fatal("want 1")
	}
}
GO
cat > "$fixture/foo/review_x_test.go" <<'GO'
package foo

import "testing"

func TestX(t *testing.T) {
	if Foo() != 1 {
		t.Error("want 1")
	}
}
GO
cat > "$fixture/bar/bar_test.go" <<'GO'
package bar

import "testing"

func TestBar(t *testing.T) {
	if Bar() != 2 {
		t.Error("want 2")
	}
}
GO
git -C "$fixture" add -A
git -C "$fixture" commit -qm 'fixture: two packages, one file named for a round'

# The table the cases start from is the one the tool writes for this tree: a case then breaks one
# thing about it, so "the refusal comes from the break and not from the shape" is settled.
write() { python3 "$checker" --root "$fixture" --markdown - --write "$@" >/dev/null; }
# ask and tool leave the checker's answer in `asked` and its exit status in `ASK_RC`. `ask` reads
# stderr only: its success line is on stdout, and a case that asks "did this print nothing" must not
# be answered by the line saying nothing was refused.
#
# The status is asked because it is half the refusal. scripts/check_test_inventory.sh execs the tool
# and scripts/check_push_tier.sh substitutes it, so the exit status — and not the sentence a person
# reads in a log — is what stops a merge. Grepping the sentence while the pipeline swallowed the
# status is how every case below stayed green over a tool that refused and exited 0 (review 1,
# finding 2); each case now asks the sentence and the status, and the last case asks that a clean
# tree exits 0 so a non-zero status cannot become a habit the pin never questions.
ask() {
	ASK_RC=0
	asked="$(python3 "$checker" --root "$fixture" --markdown - --check 2>&1 1>/dev/null)" || ASK_RC=$?
}
tool() {
	ASK_RC=0
	asked="$(python3 "$checker" --root "$fixture" --markdown - "$@" 2>&1)" || ASK_RC=$?
}
refused() {
	if ! grep -q -- "$1" <<<"$asked"; then
		note "no refusal named: $1 — the tool printed: $(printf '%s\n' "$asked" | head -1)"
	elif [ "$ASK_RC" -eq 0 ]; then
		note "$1 was printed and the tool exited 0: a check that cannot fail a gate"
	fi
}
edit() { python3 - "$fixture/tests/inventory.json" "$@"; }

write --ceiling 2
ask
if [ -n "$asked" ] || [ "$ASK_RC" -ne 0 ]; then
	note "the fixture's own table is refused: $(printf '%s\n' "$asked" | head -1)"
fi

# 1. a test with no row is named, by id, and the status says so.
cp "$fixture/tests/inventory.json" "$fixture/keep.json"
edit <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
d["rows"] = [r for r in d["rows"] if r["id"] != "foo/foo_test.go#TestFoo"]
json.dump(d, open(sys.argv[1], "w"), indent=1, sort_keys=True)
PY
ask
refused 'no row for the test in the tree: foo/foo_test.go#TestFoo'

# 2. a row whose test no longer exists is named.
cp "$fixture/keep.json" "$fixture/tests/inventory.json"
rm "$fixture/bar/bar_test.go"
git -C "$fixture" add -A
git -C "$fixture" commit -qm 'fixture: the bar case is gone'
ask
refused 'row with no test in the tree: bar/bar_test.go#TestBar'

# 3. the ratchet: a rise above the ceiling is refused, a fall passes, and --write never raises it.
git -C "$fixture" checkout -q HEAD~1 -- bar/bar_test.go
cat > "$fixture/foo/review_y_test.go" <<'GO'
package foo

import "testing"

func TestY(t *testing.T) {
	t.Error("another round, another file")
}
GO
git -C "$fixture" add -A
git -C "$fixture" commit -qm 'fixture: a second file named for a round'
write --ceiling 1
ask
refused 'round-named test files grew to 2 above the ceiling 1'
tool --write --ceiling 9
refused 'only lowers 1'
rm "$fixture/foo/review_y_test.go"
git -C "$fixture" add -A
git -C "$fixture" commit -qm 'fixture: the second file named for a round is pruned'
write
ask
if [ -n "$asked" ] || [ "$ASK_RC" -ne 0 ]; then
	note "the same tree after the fall was refused: $(printf '%s\n' "$asked" | head -1)"
fi
cp "$fixture/tests/inventory.json" "$fixture/keep.json"

# 4. a delete beside an unmeasured coverage column is refused: the checker stops it, not a person.
edit <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
for r in d["rows"]:
    if r["id"] == "foo/foo_test.go#TestFoo":
        r["verdict"], r["unique_lines"] = "delete", None
json.dump(d, open(sys.argv[1], "w"), indent=1, sort_keys=True)
PY
ask
refused 'a delete beside an unmeasured coverage column is refused: foo/foo_test.go#TestFoo'

# 5. a merge naming a file that is not in the tree is refused.
cp "$fixture/keep.json" "$fixture/tests/inventory.json"
edit <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
for r in d["rows"]:
    if r["id"] == "foo/review_x_test.go#TestX":
        r["verdict"], r["merge_into"] = "merge", "foo/kept_sibling_test.go"
json.dump(d, open(sys.argv[1], "w"), indent=1, sort_keys=True)
PY
ask
refused 'merge_into names a file that is not in the tree'

# 6. the generator is a fixed point: the same tree twice writes the same bytes, so CI's re-check
#    compares a table with a tree and never with the order a dict happened to iterate in.
cp "$fixture/keep.json" "$fixture/tests/inventory.json"
write
cp "$fixture/tests/inventory.json" "$fixture/first.json"
write
if ! cmp -s "$fixture/first.json" "$fixture/tests/inventory.json"; then
	note "--write is not a fixed point: re-running it on an unchanged tree changed the bytes"
fi
ask
if [ -n "$asked" ] || [ "$ASK_RC" -ne 0 ]; then
	note "the tree the generator just wrote is refused by the checker: $(printf '%s\n' "$asked" | head -1)"
fi

# 7. the push tier refuses a selection that opens a stack. Its promise is "no database", and a
#    package that needs one is not in it whatever a row's tier column claims.
edit <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
for r in d["rows"]:
    if r["pkg"] == "foo":
        r["tier"], r["needs_db"] = "push", True
json.dump(d, open(sys.argv[1], "w"), indent=1, sort_keys=True)
PY
tool --tier push --base ''
refused 'reaches packages that open a stack'

# 8. a verdict outside the four, and a table that is not the schema at all.
cp "$fixture/keep.json" "$fixture/tests/inventory.json"
edit <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
d["rows"][0]["verdict"] = "probably-fine"
json.dump(d, open(sys.argv[1], "w"), indent=1, sort_keys=True)
PY
ask
refused 'verdict .probably-fine. is not one of'

# 9. the status is a verdict and not a habit: the tree the table answers exits 0. An exit non-zero
#    on every call would pass every case above while refusing every run, which is the same useless
#    gate wearing the other coat.
cp "$fixture/keep.json" "$fixture/tests/inventory.json"
ask
if [ -n "$asked" ] || [ "$ASK_RC" -ne 0 ]; then
	note "a table the tree answers was refused: $(printf '%s\n' "$asked" | head -1)"
fi
tool --tier push --base ''
if [ "$ASK_RC" -ne 0 ]; then
	note "the push tier refused the tree its table answers: $(printf '%s\n' "$asked" | head -1)"
fi

if [ "$fails" -ne 0 ]; then
	printf '%d case(s) of scripts/check_test_inventory.sh are not pinned\n' "$fails" >&2
	exit 1
fi
echo "ok — scripts/check_test_inventory.sh refuses a missing row, a missing test, a round-named rise,"
echo "     an unmeasured delete, a merge naming nothing, a verdict outside the four, a push tier that"
echo "     opens a stack, and a generator that is not a fixed point — and every one of those refusals"
echo "     is an exit status a gate acts on, not only a sentence a person reads"
