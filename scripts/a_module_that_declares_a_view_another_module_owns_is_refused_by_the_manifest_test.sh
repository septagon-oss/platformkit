#!/usr/bin/env bash
# A module's own view descriptors are checked while its manifest is checked.
#
# module.Module.Views carries a custom screen's immutable descriptors, and
# module.Validate is the first place that asks whether a module may declare them:
# every ref must parse, must be owned by the module that declares it, and must be
# named once. screen.Publish refuses a mount whose view belongs to somebody else,
# so a composition that reached routing with a foreign view was already refused —
# but at the wrong door, with the manifest's own diagnostics gone and the sentence
# naming an operation rather than the module that wrote the declaration.
#
# Nothing in the tree asks the manifest question: the refusal sentence appears in no
# test, and the composition gate that does catch a missing route-level screen check
# (pkit's anchor refusal) answers to this one not at all. So this case asks it
# through a Go file the checkout never carries, added over Go's build overlay, and
# then refuses the reading in which the manifest stops asking: the same case has to
# fail once the check is taken out, else it answers to no decision.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
cd "$root"
failures=0
fail() { echo "FAIL: $1"; failures=$((failures + 1)); }
ok() { echo "ok   $1"; }

# overlay <out.json> <destination>=<source>... — the JSON cmd/go reads to put a file
# where the checkout has none, or to read a source from elsewhere. Paths are made
# absolute, which is the form cmd/go requires.
overlay() {
	local out="$1"; shift
	python3 -c '
import json, os, sys
out, entries = sys.argv[1], sys.argv[2:]
m = {}
for entry in entries:
    dst, src = entry.split("=", 1)
    m[os.path.abspath(dst)] = os.path.abspath(src)
json.dump({"Replace": m}, open(out, "w"))
' "$out" "kit/module/zz_view_ownership_case_test.go=$work/ownership_case.go" "$@"
}

cat >"$work/ownership_case.go" <<'GO'
package module_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/screen"
)

// declared builds one custom view descriptor the screen contract accepts, so the
// manifest is asked about a value it would meet in a real module.
func declared(t *testing.T, ref string) screen.View {
	t.Helper()
	parsed, err := screen.ParseView(ref)
	if err != nil {
		t.Fatalf("ParseView(%q): %v", ref, err)
	}
	view, err := screen.NewView(screen.ViewSpec{Ref: parsed, Cardinality: screen.One})
	if err != nil {
		t.Fatalf("NewView(%q): %v", ref, err)
	}
	return view
}

// TestAManifestRefusesAViewDescriptorAnotherModuleOwnsOrNamesTwice keeps ownership of
// a published view with the module that declares it: a module that declares its own
// view is accepted, one that declares a view another owner names is refused, and one
// that names the same view twice is refused, because a repeated ref would publish one
// anchor twice under one name.
func TestAManifestRefusesAViewDescriptorAnotherModuleOwnsOrNamesTwice(t *testing.T) {
	t.Parallel()
	own := declared(t, "app/memo/custom/board")
	foreign := declared(t, "app/other/custom/board")
	again := declared(t, "app/memo/custom/board")

	if err := module.Validate([]module.Module{{Name: "memo", Views: []screen.View{own}}}); err != nil {
		t.Errorf("a module declaring its own view is refused: %v", err)
	}
	err := module.Validate([]module.Module{{Name: "memo", Views: []screen.View{foreign}}})
	if err == nil || !strings.Contains(err.Error(), "duplicate or foreign view app/other/custom/board") {
		t.Errorf("a module declaring app/other/custom/board is accepted: Validate() = %v, want a refusal naming that view", err)
	}
	err = module.Validate([]module.Module{{Name: "memo", Views: []screen.View{own, again}}})
	if err == nil || !strings.Contains(err.Error(), "duplicate or foreign view app/memo/custom/board") {
		t.Errorf("a module naming one view twice is accepted: Validate() = %v, want a refusal naming that view", err)
	}
	// Two owners, each declaring the view its own name owns, is the ordinary case and
	// stays accepted: the refusal above is about ownership, not about two modules
	// having screens at all.
	if err := module.Validate([]module.Module{
		{Name: "memo", Views: []screen.View{own}},
		{Name: "other", Views: []screen.View{foreign}},
	}); err != nil {
		t.Errorf("two modules each declaring their own view are refused: %v", err)
	}
}
GO

overlay "$work/case.json"
if go test -overlay "$work/case.json" -count=1 \
	-run '^TestAManifestRefusesAViewDescriptorAnotherModuleOwnsOrNamesTwice$' ./kit/module </dev/null >"$work/case.log" 2>&1; then
	ok "the manifest refuses a view another module owns and a view named twice, and accepts each module's own"
else
	fail "the manifest does not answer the way the case reads it: $(tail -6 "$work/case.log" | tr '\n' ' ')"
fi

# The same case, asked of a validate.go that stopped checking views. An edit that
# matched nothing would leave the refusal below asking nothing, so the copy is
# compared before it is judged.
cut=0
python3 - "$PWD/kit/module/validate.go" "$work/validate_unchecked.go" <<'PY' || cut=$?
import sys
src, out = sys.argv[1], sys.argv[2]
needle = '\tif err := validateViews(m); err != nil {\n\t\tv.add("%v", err)\n\t}\n'
body = open(src).read()
if needle not in body:
    sys.exit(3)
open(out, "w").write(body.replace(needle, "", 1))
PY
cut=${cut:-0}
if [ "$cut" -eq 3 ]; then
	fail "validate.go no longer holds the view-descriptor check in the shape this case reads; the case above asks nothing"
elif [ "$cut" -ne 0 ]; then
	fail "the copy of validate.go could not be written (exit $cut)"
elif overlay "$work/mutant.json" "kit/module/validate.go=$work/validate_unchecked.go" &&
	go test -overlay "$work/mutant.json" -count=1 \
		-run '^TestAManifestRefusesAViewDescriptorAnotherModuleOwnsOrNamesTwice$' ./kit/module </dev/null >"$work/mutant.log" 2>&1; then
	fail "a validate.go that stopped checking view descriptors still passes the case: the case answers to nothing"
else
	ok "the same case refuses a manifest that stopped checking its view descriptors"
fi

[ "$failures" -eq 0 ] || { echo "$failures case(s) failed"; exit 1; }
echo 'the manifest refuses a view descriptor another module owns or names twice: all cases hold'
