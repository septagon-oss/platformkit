#!/usr/bin/env bash
# Two things decide whether the served page's write-door pin keeps answering, and nothing else in
# the tree asks either.
#
# First, that `make check` still runs it. tests/inventory.json files a scripts/ pin by its path
# alone, so a recipe that dropped the line would leave the row, the file and every figure of the
# inventory intact — only the recipe line makes the pin run.
#
# Second, that both of the pin's runs report a verdict for the case they inject. The pin asks one
# pair of pages twice — once with every write closure wired, once with none — and reads the exit
# status of each `go test` as its answer. A run that reported nothing would still answer: an
# overlay that stopped applying leaves `go test` free to compile the package, match no case and
# exit zero, and a copy of the renderer that no longer compiles is likewise non-zero. Either way
# the pin would print that the pages answered and ask nothing. This case requires the injected
# case's own PASS and FAIL lines, so the day an overlay or a mutation stops reaching the case the
# recipe refuses rather than stays green.
#
# Both halves are asked of a copy of the case the pin injects over Go's build overlay, never of
# this checkout: the file the pin adds to ui/screens is extracted from the pin itself, so this
# case judges the case the pin asks rather than a paraphrase of it.
set -euo pipefail
cd "$(dirname "$0")/.."
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
failures=0
fail() { echo "FAIL: $1"; failures=$((failures + 1)); }
ok() { echo "ok   $1"; }

pin='scripts/a_served_page_names_no_write_door_behind_a_route_it_does_not_mount_test.sh'
case_name='TestAServedPageDrawsNoWriteDoorForAWriteRouteTheAdapterDoesNotMount'
recipe_line="bash $pin"

# The recipe make would run, and the same recipe with only that line dropped.
recipe="$(make -n check </dev/null)"
held="$(grep -Fxc "$recipe_line" <<<"$recipe" || true)"
dropped="$(grep -Fxc "$recipe_line" <<<"${recipe//$recipe_line/true}" || true)"
if [ "$held" -eq 1 ]; then
	ok "make check runs $pin"
else
	fail "make check does not run the served page's write-door pin exactly once: '$recipe_line' appears $held time(s)"
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

# The pair of pages of the tree as it stands: the run must answer, and answer with a verdict.
overlay "$work/clean.json" "ui/screens/zz_served_write_doors_test.go=$work/injected_case.go"
if go test -overlay "$work/clean.json" -count=1 -v -run "^${case_name}\$" ./ui/screens </dev/null \
	>"$work/clean.log" 2>&1 && grep -q -- "--- PASS: $case_name" "$work/clean.log"; then
	ok "the served pages answer the injected case, and the case reports its own pass"
else
	fail "the run of the tree as it stands reported no verdict for $case_name: $(tail -4 "$work/clean.log" | tr '\n' ' ')"
fi

# The renderer asked of what the resource declares instead of what the adapter mounts — the same
# single edit the pin makes, made here so a copy that stopped compiling cannot be read as the
# refusal. If the line moves, this case refuses rather than judge a copy that was never mutated.
sed 's|} else if writeAvailable(r, verb) {|} else if r.Offers(verb) {|' ui/screens/render.go >"$work/declared.go"
if cmp -s ui/screens/render.go "$work/declared.go"; then
	fail "ui/screens/render.go no longer holds the call the pin's copy replaces in the shape this case reads"
else
	overlay "$work/declared.json" \
		"ui/screens/zz_served_write_doors_test.go=$work/injected_case.go" \
		"ui/screens/render.go=$work/declared.go"
	go test -overlay "$work/declared.json" -count=1 -v -run "^${case_name}\$" ./ui/screens </dev/null \
		>"$work/declared.log" 2>&1 || true
	if grep -q -- "--- FAIL: $case_name" "$work/declared.log" && ! grep -q "\[build failed\]" "$work/declared.log"; then
		ok "the copy that reads declarations is refused by the case's own assertion"
	else
		fail "the declared reading was not refused by an assertion: $(tail -4 "$work/declared.log" | tr '\n' ' ')"
	fi
fi

[ "$failures" -eq 0 ] || { echo "$failures case(s) failed"; exit 1; }
echo 'the served page door pin is a check recipe line, and both of its runs report their own verdict: all cases hold'
