#!/usr/bin/env bash
# Six refusals of scripts/check_test_inventory.sh, pinned against a fake tree.
#
# The checker reads the checkout and tests/inventory.json and never a database, so the fixture
# these cases need is a directory with two test files in it — the shape
# scripts/check_budget_ratchet_test.sh uses for the ratchet, and the reason a checker over tests
# needs no Postgres to prove what it refuses (decision 0088).
#
# Each case names the answer that decides it, and each one fails for a stated reason: an assertion
# here that could not fail is the same as the column that quoted nine wrong deletes.
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
# ask prints the checker's refusals and nothing else: its success line is on stdout, and a case that
# asks "did this print nothing" must not be answered by the line saying nothing was refused.
ask() { python3 "$checker" --root "$fixture" --markdown - --check 2>&1 1>/dev/null; }
edit() { python3 - "$fixture/tests/inventory.json" "$@"; }

write --ceiling 2
if [ -n "$(ask)" ]; then
	note "the fixture's own table is refused: $(ask | head -1)"
fi

# 1. a test with no row is named, by id.
cp "$fixture/tests/inventory.json" "$fixture/keep.json"
edit <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
d["rows"] = [r for r in d["rows"] if r["id"] != "foo/foo_test.go#TestFoo"]
json.dump(d, open(sys.argv[1], "w"), indent=1, sort_keys=True)
PY
if ! ask | grep -q 'no row for the test in the tree: foo/foo_test.go#TestFoo'; then
	note "a test with no row passed: case 1 is not pinned"
fi

# 2. a row whose test no longer exists is named.
cp "$fixture/keep.json" "$fixture/tests/inventory.json"
rm "$fixture/bar/bar_test.go"
git -C "$fixture" add -A
git -C "$fixture" commit -qm 'fixture: the bar case is gone'
if ! ask | grep -q 'row with no test in the tree: bar/bar_test.go#TestBar'; then
	note "a row with no test passed: case 2 is not pinned"
fi

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
if ! ask | grep -q 'round-named test files grew to 2 above the ceiling 1'; then
	note "a round-named rise above the ceiling passed: case 3 is not pinned"
fi
if ! python3 "$checker" --root "$fixture" --markdown - --write --ceiling 9 2>&1 | grep -q 'only lowers 1'; then
	note "--write raised the ceiling from 1: the ratchet turns both ways, which is not a ratchet"
fi
rm "$fixture/foo/review_y_test.go"
git -C "$fixture" add -A
git -C "$fixture" commit -qm 'fixture: the second file named for a round is pruned'
write
if [ -n "$(ask)" ]; then
	note "the same tree after the fall was refused: $(ask | head -1)"
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
if ! ask | grep -q 'a delete beside an unmeasured coverage column is refused: foo/foo_test.go#TestFoo'; then
	note "an unmeasured delete passed: case 4 is not pinned"
fi

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
if ! ask | grep -q 'merge_into names a file that is not in the tree'; then
	note "a merge naming a missing target passed: case 5 is not pinned"
fi

# 6. the generator is a fixed point: the same tree twice writes the same bytes, so CI's re-check
#    compares a table with a tree and never with the order a dict happened to iterate in.
cp "$fixture/keep.json" "$fixture/tests/inventory.json"
write
cp "$fixture/tests/inventory.json" "$fixture/first.json"
write
if ! cmp -s "$fixture/first.json" "$fixture/tests/inventory.json"; then
	note "--write is not a fixed point: re-running it on an unchanged tree changed the bytes"
fi
if [ -n "$(ask)" ]; then
	note "the tree the generator just wrote is refused by the checker: $(ask | head -1)"
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
if ! python3 "$checker" --root "$fixture" --markdown - --tier push --base '' 2>&1 | grep -q 'reaches packages that open a stack'; then
	note "the push tier accepted a package that opens a stack: case 7 is not pinned"
fi

# 8. a verdict outside the four, and a table that is not the schema at all.
cp "$fixture/keep.json" "$fixture/tests/inventory.json"
edit <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
d["rows"][0]["verdict"] = "probably-fine"
json.dump(d, open(sys.argv[1], "w"), indent=1, sort_keys=True)
PY
if ! ask | grep -q 'verdict .probably-fine. is not one of'; then
	note "a verdict outside the four passed: case 8 is not pinned"
fi

if [ "$fails" -ne 0 ]; then
	printf '%d case(s) of scripts/check_test_inventory.sh are not pinned\n' "$fails" >&2
	exit 1
fi
echo "ok — scripts/check_test_inventory.sh refuses a missing row, a missing test, a round-named rise,"
echo "     an unmeasured delete, a merge naming nothing, a verdict outside the four, a push tier that"
echo "     opens a stack, and a generator that is not a fixed point"
