#!/usr/bin/env bash
# Two things decide whether the manifest's view-descriptor pin keeps answering, and neither is
# asked by anything else in the tree.
#
# First, that `make check` still runs it. tests/inventory.json files a scripts/ pin by its path
# alone, so a recipe that dropped the line would leave the row, the file and every figure intact —
# only the recipe line makes the pin run, and this case reads the recipe make would run and refuses
# one without it.
#
# Second, that the pin's mutation half is refused by the case rather than by a broken build. That
# pin deletes the manifest's view-descriptor call from a copy of its source and treats any non-zero
# `go test` as the case refusing the copy — so a copy that no longer compiles, or a copy step that
# failed for a reason it did not predict, would be read as the refusal the pin wants. This case
# asks the same copy for its answer and requires the assertion in it, so the day the copy stops
# compiling the recipe refuses rather than stays green.
#
# Both halves are asked of a copy of the case the pin injects over Go's build overlay, never of
# this checkout: the file the pin adds to kit/module is extracted from the pin itself, so this
# case judges the case the pin asks rather than a paraphrase of it.
set -euo pipefail
cd "$(dirname "$0")/.."
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
failures=0
fail() { echo "FAIL: $1"; failures=$((failures + 1)); }
ok() { echo "ok   $1"; }

pin='scripts/a_module_that_declares_a_view_another_module_owns_is_refused_by_the_manifest_test.sh'
case_name='TestAManifestRefusesAViewDescriptorAnotherModuleOwnsOrNamesTwice'
recipe_line="bash $pin"

# The recipe make would run, and the same recipe with only that line dropped.
recipe="$(make -n check </dev/null)"
held="$(grep -Fxc "$recipe_line" <<<"$recipe" || true)"
dropped="$(grep -Fxc "$recipe_line" <<<"${recipe//$recipe_line/true}" || true)"
if [ "$held" -eq 1 ]; then
	ok "make check runs $pin"
else
	fail "make check does not run the manifest's view-descriptor pin exactly once: '$recipe_line' appears $held time(s)"
fi

if [ "$held" -eq 1 ] && [ "$dropped" -eq 0 ]; then
	ok "a recipe with that line dropped is a recipe this case refuses"
else
	fail "dropping the pin's recipe line left $dropped copy(ies) of it in the recipe: the reading asks nothing"
fi

# The case the pin injects, extracted from the pin so the two cannot drift apart.
python3 - "$pin" "$work/injected_case.go" <<'PY'
import sys
src, out = sys.argv[1], sys.argv[2]
body, inside = [], False
for line in open(src):
    if line.startswith("cat >") and line.rstrip("\n").endswith("<<'GO'"):
        inside = True
        continue
    if inside and line.rstrip("\n") == "GO":
        break
    if inside:
        body.append(line)
open(out, "w").writelines(body)
PY
grep -Fq "func $case_name" "$work/injected_case.go" &&
	ok "the pin still carries its injected case in the shape this case reads" ||
	fail "the pin no longer carries a Go case named $case_name; nothing here judges it"

# overlay <out.json> <destination>=<source>... — the JSON cmd/go reads to read a source from
# elsewhere; paths absolute, which is the form cmd/go requires.
overlay() {
	local out="$1"
	shift
	python3 -c '
import json, os, sys
out, entries = sys.argv[1], sys.argv[2:]
m = {}
for entry in entries:
    dst, src = entry.split("=", 1)
    m[os.path.abspath(dst)] = os.path.abspath(src)
json.dump({"Replace": m}, open(out, "w"))
' "$out" "$@"
}

overlay "$work/clean.json" "kit/module/zz_view_ownership_case_test.go=$work/injected_case.go"
if go test -overlay "$work/clean.json" -count=1 -run "^${case_name}\$" ./kit/module </dev/null >"$work/clean.log" 2>&1; then
	ok "the injected case passes the manifest as it stands"
else
	fail "the injected case does not pass the manifest as it stands: $(tail -4 "$work/clean.log" | tr '\n' ' ')"
fi

# The copy that stopped checking views: the same three lines the pin deletes, deleted here. If the
# manifest moves them, this case refuses rather than judge a copy that was never mutated.
if ! python3 - kit/module/validate.go "$work/unchecked.go" <<'PY'
import sys
src, out = sys.argv[1], sys.argv[2]
needle = '\tif err := validateViews(m); err != nil {\n\t\tv.add("%v", err)\n\t}\n'
body = open(src).read()
if needle not in body:
    sys.exit(3)
open(out, "w").write(body.replace(needle, "", 1))
PY
then
	fail "kit/module/validate.go no longer holds the view-descriptor call in the shape this case reads"
else
	overlay "$work/mutant.json" \
		"kit/module/zz_view_ownership_case_test.go=$work/injected_case.go" \
		"kit/module/validate.go=$work/unchecked.go"
	go test -overlay "$work/mutant.json" -count=1 -run "^${case_name}\$" ./kit/module </dev/null \
		>"$work/mutant.log" 2>&1 || true
	if grep -q -- "--- FAIL: $case_name" "$work/mutant.log" && ! grep -q "\[build failed\]" "$work/mutant.log"; then
		ok "the copy that stopped checking views is refused by the case's own assertion"
	else
		fail "the copy that stopped checking views was not refused by an assertion: $(tail -4 "$work/mutant.log" | tr '\n' ' ')"
	fi
fi

[ "$failures" -eq 0 ] || { echo "$failures case(s) failed"; exit 1; }
echo 'the manifest view pin is a check recipe line, and its mutant is refused by assertion: all cases hold'
