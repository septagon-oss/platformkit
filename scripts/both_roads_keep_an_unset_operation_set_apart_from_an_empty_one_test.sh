#!/usr/bin/env bash
# One registration, two roads, one answer about which routes it mounted.
#
# Resource.Operations answers "is this verb mounted?" from two different facts, and
# the pair has to stay apart: a nil set is the absence of a declaration, so all five
# are mounted — which is what a screen rendered with no kernel behind it (a design
# export, a golden file) declares — while an empty set is a list that named nothing,
# so none is. Answering from the length of the slice rather than from its presence
# merges the two, and one registration then answers one way for an export and another
# for a mounted page: a door, or a draft address, behind no route.
#
# Two packages decide from that pair. ui/resource decides which controls to draw, and
# TestAnEmptyOperationListMountsNoRouteWhereNilMountsAllFive holds the four readings
# apart at the rendered markup. kit/entity/present decides which views exist at all
# (views.go), and nothing held the pair there. This case asks both roads, and refuses
# the merged reading on each: the render road by running the case the repository
# already carries, the resolve road by adding a case over Go's build overlay, so the
# question is answered by a real present.Resolve rather than by a grep of the source.
# An overlaid path is one the build sees and the checkout does not, so the refusals
# below edit copies under the overlay and never this checkout's files.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
cd "$root"
failures=0
fail() { echo "FAIL: $1"; failures=$((failures + 1)); }
ok() { echo "ok   $1"; }

# The render road, as the repository already holds it. A case that left this package
# would quietly stop asking, so it is named rather than assumed present.
render_case=TestAnEmptyOperationListMountsNoRouteWhereNilMountsAllFive
render_file=ui/resource/which_routes_are_mounted_test.go

# overlay_case <out.json> [additional <destination>=<source>]... — the JSON Go reads to
# put one added test file into kit/entity/present, plus whatever file the case is about
# to be refused on. Paths are made absolute, which is the form cmd/go requires.
overlay_case() {
	local out="$1"; shift
	python3 -c '
import json, os, sys
out, entries = sys.argv[1], sys.argv[2:]
m = {}
for entry in entries:
    dst, src = entry.split("=", 1)
    m[os.path.abspath(dst)] = os.path.abspath(src)
json.dump({"Replace": m}, open(out, "w"))
' "$out" "kit/entity/present/zz_operation_pair_test.go=$work/pair_test.go" "$@"
}

cat >"$work/pair_test.go" <<'GO'
package present_test

import (
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
)

// TestAnEmptyOperationSetResolvesNoViewWhereNilResolvesAllFive keeps the two readings
// of an unset operation set apart at the presentation kernel: nil is the absence of a
// declaration, so the list, the record, the new form and the edit form all resolve; an
// empty slice is a list that named nothing, so no view resolves and no shell is handed
// a screen whose route was never mounted. The named set is the third answer and the
// one a mounted resource most often carries: the verbs it names, in any order, resolve
// exactly the views those verbs lead to.
func TestAnEmptyOperationSetResolvesNoViewWhereNilResolvesAllFive(t *testing.T) {
	t.Parallel()
	in := present.Input{Schema: entity.Schema{Module: "memo", Entity: "memo",
		Path: "/api/v1/memo/memos",
		Fields: []entity.Field{{Name: "title", Type: entity.TypeString}}}}
	kinds := func(operations []string) []string {
		in.Operations = operations
		out := []string{}
		for _, view := range present.Resolve(in).Views {
			out = append(out, view.Kind)
		}
		slices.Sort(out)
		return out
	}
	if got := kinds(nil); !slices.Equal(got, []string{"create", "detail", "edit", "list"}) {
		t.Errorf("a resource that named no operation set resolves views %v, want the four a caller may reach", got)
	}
	if got := kinds([]string{}); len(got) != 0 {
		t.Errorf("a resource whose operation list named nothing resolves views %v, want none: a view behind no route is a refusal with a button on it", got)
	}
	if got := kinds([]string{"update", "read"}); !slices.Equal(got, []string{"detail", "edit"}) {
		t.Errorf("a resource naming {update, read} resolves views %v, want detail and edit alone", got)
	}
}
GO

if [ ! -f "$render_file" ]; then
	fail "$render_file no longer holds the case that reads the four operation sets apart"
elif go test -count=1 -run "^${render_case}\$" ./ui/resource </dev/null >"$work/render.log" 2>&1; then
	ok "ui/resource draws no write control beside an operation set that named none"
else
	fail "ui/resource refuses the case it carries: $(tail -3 "$work/render.log" | tr '\n' ' ')"
fi

overlay_case "$work/pair.json"
if go test -overlay "$work/pair.json" -count=1 \
	-run '^TestAnEmptyOperationSetResolvesNoViewWhereNilResolvesAllFive$' ./kit/entity/present </dev/null >"$work/resolve.log" 2>&1; then
	ok "kit/entity/present resolves no view beside an operation set that named none"
else
	fail "kit/entity/present does not resolve the pair the renderers answer: $(tail -4 "$work/resolve.log" | tr '\n' ' ')"
fi

# The merged reading, in a copy only, asked of each road in turn. An edit that matched
# nothing would leave the refusal below asking nothing, so the copy is compared before
# it is judged — and a case that passes either way answers to no decision at all.
sed 's|return in.Operations == nil |return len(in.Operations) == 0 |' kit/entity/present/resolve.go >"$work/resolve_merged.go"
if cmp -s kit/entity/present/resolve.go "$work/resolve_merged.go"; then
	fail "the merged reading matched nothing in kit/entity/present/resolve.go; the case above asked nothing"
elif overlay_case "$work/resolve_mutant.json" "kit/entity/present/resolve.go=$work/resolve_merged.go" &&
	go test -overlay "$work/resolve_mutant.json" -count=1 \
		-run '^TestAnEmptyOperationSetResolvesNoViewWhereNilResolvesAllFive$' ./kit/entity/present </dev/null >"$work/resolve_mutant.log" 2>&1; then
	fail "a resolve.go that reads the set by its length still resolves the pair: the case answers to nothing"
else
	ok "the same case refuses a resolve.go that answers an unset set by its length"
fi

sed 's|if r.Operations == nil {|if len(r.Operations) == 0 {|' ui/resource/resource.go >"$work/resource_merged.go"
if cmp -s ui/resource/resource.go "$work/resource_merged.go"; then
	fail "the merged reading matched nothing in ui/resource/resource.go; the case above asked nothing"
elif overlay_case "$work/render_mutant.json" "ui/resource/resource.go=$work/resource_merged.go" &&
	go test -overlay "$work/render_mutant.json" -count=1 -run "^${render_case}\$" ./ui/resource </dev/null >"$work/render_mutant.log" 2>&1; then
	fail "the committed render case still passes with the two readings merged: it answers to nothing"
else
	ok "the committed render case refuses a resource.go that answers an unset set by its length"
fi

[ "$failures" -eq 0 ] || { echo "$failures case(s) failed"; exit 1; }
echo 'both roads keep an unset operation set apart from an empty one: all cases hold'
