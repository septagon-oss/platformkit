#!/usr/bin/env bash
# A refusal scripts/test_inventory.py prints is also its exit status.
#
# scripts/check_test_inventory.sh is a `check:` prerequisite and a nightly step, and
# scripts/check_push_tier.sh reads the push tier's selection out of the same tool: both
# depend on the tool's exit status, not on a sentence a person reads in a log. The tool's
# own docstring says "Exit 1, with every refusal named" and the push tier's wrapper says a
# selection that opens a stack is "refused with exit 3". Pinned against a fixture tree:
# the refusal must be printed (so the case reached it) and the status must be non-zero.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
checker="$root/scripts/test_inventory.py"
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT

export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null
export GIT_AUTHOR_NAME=fixture GIT_AUTHOR_EMAIL=f@invalid GIT_COMMITTER_NAME=fixture GIT_COMMITTER_EMAIL=f@invalid

mkdir -p "$fixture/foo" "$fixture/tests"
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
git -C "$fixture" add -A
git -C "$fixture" commit -qm 'fixture: one package, one case'
python3 "$checker" --root "$fixture" --markdown - --write --ceiling 0 >/dev/null 2>&1

fails=0
note() { printf 'FAIL: %s\n' "$*" >&2; fails=$((fails + 1)); }

# 1. A test with no row: the refusal is printed, and the exit status says so.
cp "$fixture/tests/inventory.json" "$fixture/keep.json"
python3 - "$fixture/tests/inventory.json" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
d["rows"] = [r for r in d["rows"] if r["id"] != "foo/foo_test.go#TestFoo"]
json.dump(d, open(sys.argv[1], "w"), indent=1, sort_keys=True)
PY
rc=0
python3 "$checker" --root "$fixture" --markdown - --check >/dev/null 2>"$fixture/check.err" || rc=$?
if ! grep -q 'no row for the test in the tree: foo/foo_test.go#TestFoo' "$fixture/check.err"; then
	note "the check did not reach the refusal: $(head -1 "$fixture/check.err")"
elif [ "$rc" -eq 0 ]; then
	note "the check printed a refusal and exited 0: a gate that cannot fail"
fi

# 2. A push-tier selection that opens a stack: the refusal is printed, and the exit status
#    is what the wrapper acts on.
cp "$fixture/keep.json" "$fixture/tests/inventory.json"
python3 - "$fixture/tests/inventory.json" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
for r in d["rows"]:
    r["tier"], r["needs_db"] = "push", True
json.dump(d, open(sys.argv[1], "w"), indent=1, sort_keys=True)
PY
rc=0
python3 "$checker" --root "$fixture" --markdown - --tier push --base '' >"$fixture/tier.out" 2>"$fixture/tier.err" || rc=$?
if ! grep -q 'reaches packages that open a stack' "$fixture/tier.err"; then
	note "the push tier did not reach the refusal: $(head -1 "$fixture/tier.err")"
elif [ "$rc" -eq 0 ]; then
	note "the push tier printed a refusal and exited 0: the wrapper reads an empty selection and runs nothing, green"
fi

# 3. The same tool on a table the tree answers exits 0, so the status is a verdict and not a habit.
cp "$fixture/keep.json" "$fixture/tests/inventory.json"
if ! python3 "$checker" --root "$fixture" --markdown - --check >/dev/null 2>&1; then
	note "a table the tree answers was refused by exit status"
fi

if [ "$fails" -ne 0 ]; then
	printf '%d assertion(s) about the inventory tool'"'"'s exit status failed\n' "$fails" >&2
	exit 1
fi
echo "ok — a refusal scripts/test_inventory.py prints is also its exit status, for --check and for --tier push"
