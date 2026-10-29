#!/usr/bin/env bash
# The pseudo-locale number refuses a chrome name whose shortcut sits outside the wrap.
#
# ui/legible is the measuring half of the gate, and what it counts as reached is the
# number in testdata/i18n-coverage.json. A page buys that number by putting the pseudo
# locale's mark around a string; the one way it could buy it without translating
# anything is to write marked copy next to words no catalogue answered and let the scan
# call the pair reached. This task is about exactly that shape in the page's own words —
# a toolbar name built as a title plus a shortcut — so the measuring half is asked the
# same question here: a name whose shortcut stands outside the wrap is copy, however
# much marked copy it touches.
#
# The question has to be asked twice, because the answer depends on a second thing. The
# scan decides `reached` per collected string, and a served body's text runs are grouped
# by the HTML parser: an end tag that closes nothing may leave the marked run and the
# words after it as two strings or coalesce them into one (golang.org/x/net's coalescing
# step, the code GO-2026-6860's fix rewrote, which this branch raised to v0.62.0). Each
# grouping hands the gate a different number of strings, so the case asserts what holds
# under both — no string that carries an unmarked shortcut reads as reached, no run a
# person reads is dropped — and, for the shape no parser can join (the shortcut past a
# real element), the exact count.
#
# The second half is the mutation the first needs: a scanner that calls any string
# merely *containing* the mark's open delimiter reached passes for the joined shape, so
# that copy is refused, and the named case is required to have run at all, so an overlay
# that reached nothing could never print an `ok`.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
cd "$root"
failures=0
fail() { echo "FAIL: $1"; failures=$((failures + 1)); }
ok() { echo "ok   $1"; }

case_name=TestThePseudoLocaleNumberRefusesANameWhoseShortcutSitsOutsideTheWrap

# overlay_of <out.json> [<destination>=<source>]... — the JSON cmd/go reads to add one test
# file to ui/legible and, for each further copy, to replace a file it reads with.
overlay_of() {
	local out="$1"; shift
	python3 -c '
import json, os, sys
out, entries = sys.argv[1], sys.argv[2:]
m = {}
for entry in entries:
    dst, src = entry.split("=", 1)
    m[os.path.abspath(dst)] = os.path.abspath(src)
json.dump({"Replace": m}, open(out, "w"))
' "$out" "ui/legible/zz_shortcut_outside_wrap_test.go=$work/shortcut_outside_wrap_test.go" "$@"
}

# ran <label> <overlay.json> <want PASS|FAIL> — the case over one overlay, and the proof it
# ran: `-run` that matches no test exits 0 and prints no `--- PASS:` line, so the log, not
# only the exit status, decides. An overlay that could not be written is a failure in its
# own right: it refuses nothing.
ran() {
	local label="$1" overlay="$2" want="$3" status=0
	if [ ! -f "$overlay" ]; then
		fail "the $label overlay could not be written, so the case below asked nothing"
		return
	fi
	go test -overlay "$overlay" -count=1 -v -run "^${case_name}\$" ./ui/legible </dev/null \
		>"$work/$label.log" 2>&1 || status=$?
	if ! grep -qE "^ *--- (PASS|FAIL): ${case_name}[ (]" "$work/$label.log"; then
		fail "$label: the named case did not run at all; $(tail -4 "$work/$label.log" | tr '\n' ' ')"
		return
	fi
	if [ "$want" = PASS ] && [ "$status" -ne 0 ]; then
		fail "$label: $(tail -8 "$work/$label.log" | tr '\n' ' ')"
		return
	fi
	if [ "$want" = FAIL ] && [ "$status" -eq 0 ]; then
		fail "$label: the case passes where it must refuse; $(tail -4 "$work/$label.log" | tr '\n' ' ')"
		return
	fi
	ok "$label"
}

cat >"$work/shortcut_outside_wrap_test.go" <<'GO'
package legible_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/locale/providers/pseudo"
	"github.com/septagon-oss/platformkit/ui/legible"
)

// TestThePseudoLocaleNumberRefusesANameWhoseShortcutSitsOutsideTheWrap asks the number
// about a name built as a title plus a shortcut, in two documents that differ in whether
// the parser can join the two runs. Both assert the same two things — every run of text a
// person reads comes back, and no string carrying an unmarked shortcut reads as reached —
// and the shape no parser can join additionally asserts that the shortcut alone is the
// page's one violation. pseudo.Mark is used rather than a literal wrap so the delimiter is
// the provider's, and the texts below are compared against it, never against a copy of it.
func TestThePseudoLocaleNumberRefusesANameWhoseShortcutSitsOutsideTheWrap(t *testing.T) {
	marked := pseudo.Mark("Bold")
	for _, table := range []struct {
		what string
		body string
		// joined is the whole text a person reads in <main>, whichever way the parser
		// grouped the runs, and exactShortcuts, when set, is how many violations the
		// document must produce and what each must name.
		joined        string
		exactShortcut string
	}{{
		what:          "the shortcut past an element the parser keeps",
		body:          "<html><body><main>" + marked + "<kbd>(Ctrl+B)</kbd></main></body></html>",
		joined:        marked + "(Ctrl+B)",
		exactShortcut: "(Ctrl+B)",
	}, {
		what:   "the shortcut joined to the name, with an end tag that closes nothing after it",
		body:   "<html><body><main>" + marked + "(Ctrl+B)</x>more</main></body></html>",
		joined: marked + "(Ctrl+B)more",
	}} {
		collected, err := legible.Scan([]byte(table.body), pseudo.Wrapped)
		if err != nil {
			t.Fatalf("the scan of %s refuses a document an ignored end tag splits: %v", table.what, err)
		}
		var texts []string
		for _, s := range collected {
			if s.Attribute != "" {
				continue
			}
			texts = append(texts, s.Text)
		}
		if got := strings.Join(texts, ""); got != table.joined {
			t.Errorf("the scan of %s hands back %q, want the text a person reads %q",
				table.what, got, table.joined)
		}
		// Whatever the grouping, a string that carries the unmarked shortcut is not a
		// string a catalogue answered, and it is the page's copy: the number must say so.
		unreachable, declined := legible.Report("GET /app/task/tasks/{id}",
			legible.MarkDatum(collected, pseudo.Wrapped, nil))
		for _, s := range collected {
			if s.Attribute == "" && s.Reached && strings.Contains(s.Text, "(Ctrl+B)") {
				t.Errorf("the scan of %s calls a string carrying an unmarked shortcut reached: %q",
					table.what, s.Text)
			}
		}
		if len(declined) != 0 {
			t.Errorf("the scan of %s declined %d string(s) as data; nothing here is a stored value: %v",
				table.what, len(declined), declined)
		}
		if table.exactShortcut == "" {
			if len(unreachable) < 1 || !anyNames(unreachable, "(Ctrl+B)") {
				t.Errorf("the scan of %s reports no copy carrying the shortcut: %v", table.what, unreachable)
			}
			continue
		}
		if len(unreachable) != 1 || !strings.Contains(unreachable[0], table.exactShortcut) {
			t.Errorf("the scan of %s reports %d copy line(s), want one naming %q: %v",
				table.what, len(unreachable), table.exactShortcut, unreachable)
		}
		if !anyNames(unreachable, table.exactShortcut) {
			t.Errorf("the scan of %s lost the shortcut from its copy: %v", table.what, unreachable)
		}
	}
}

// anyNames says whether one report line quotes the given text.
func anyNames(lines []string, text string) bool {
	for _, line := range lines {
		if strings.Contains(line, text) {
			return true
		}
	}
	return false
}
GO

overlay_of "$work/shipped.json"
ran "the number counts a chrome name with an unmarked shortcut as copy" "$work/shipped.json" PASS

# The same case of a scanner that calls any string merely holding the mark's open
# delimiter reached — the reading under which joining marked words with unmarked ones
# buys coverage. Unless this copy is refused, the `ok` above says only that a joined
# string agrees with itself.
if ! python3 - ui/legible/legible.go "$work/generous.go" <<'PY'
import sys
src, out = sys.argv[1], sys.argv[2]
needle = """	return reached != nil && reached(canon(text))"""
text = open(src).read()
if text.count(needle) != 1:
    sys.exit("%s: needle found %d time(s), want 1" % (src, text.count(needle)))
open(out, "w").write(text.replace(needle,
    '\treturn reached != nil && (reached(canon(text)) || strings.Contains(canon(text), "\\u27e6"))'))
PY
then
	fail "the generous-scanner copy could not be written, so the case below asked nothing"
else
	overlay_of "$work/generous.json" "ui/legible/legible.go=$work/generous.go"
	ran "a scanner that reads the mark inside a string as reached is refused" "$work/generous.json" FAIL
fi

[ "$failures" -eq 0 ] || { echo "$failures case(s) failed"; exit 1; }
echo 'the pseudo-locale number counts a name whose shortcut stands outside the wrap: all cases hold'
