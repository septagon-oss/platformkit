#!/usr/bin/env bash
# The pseudo-locale scanner reads a document it did not write, whole and in order.
#
# ui/legible is the measuring half of the gate: legible.Scan parses a served body with
# golang.org/x/net/html and hands back every string a person could read in it, and the
# number in testdata/i18n-coverage.json is the sum of what it hands back. That parse is
# where the gate's own floor is made, so the one thing no other case can be trusted to
# keep true is the scanner's reading of markup the repository never wrote — a body whose
# runs are split by an end tag that closes nothing, which the parser may join into one
# text node or leave as two.
#
# golang.org/x/net's coalescing step is exactly the code that changed when the
# quadratic-complexity advisory closed (GO-2026-6860, fixed in x/net v0.62.0, traced from
# legible.Scan calling html.Parse), and reading the collected strings back in document
# order says what the body contains whichever way that grouping goes. So this asks the one
# question the grouping leaves open — that the scan drops no run a person reads and
# invents no run it was not sent — and answers it twice over: of the shipped package, and
# of a copy that quietly stops reading one element's text, which must be refused. A scanner
# that reads less than a person does would report a flatter number than the pages deserve.
#
# The same call is why no reachability argument is available over that advisory from inside
# this repository: the bytes legible.Scan parses are not its own, and this file is the case
# that says so without reaching the network for a vulnerability feed no make goal can read.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
cd "$root"
failures=0
fail() { echo "FAIL: $1"; failures=$((failures + 1)); }
ok() { echo "ok   $1"; }

scan_case=TestThePseudoLocaleScannerReadsASplitDocumentWhole

# overlay_of <out.json> [<destination>=<source>]... — the JSON cmd/go reads to add one test file to
# ui/legible and, for each further copy, to replace the file it parses with. An overlaid path is one
# the build sees and the checkout does not.
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
' "$out" "ui/legible/zz_read_in_full_test.go=$work/read_in_full_test.go" "$@"
}

# ask <label> <overlay.json> <case-name> — the case, run over one overlay. An overlay that could not
# be written is a failure in its own right: it refuses nothing.
ask() {
	local label="$1" overlay="$2" name="$3"
	if [ ! -f "$overlay" ]; then
		fail "the $label overlay could not be written, so the case below asked nothing"
		return
	fi
	if go test -overlay "$overlay" -count=1 -run "^${name}\$" ./ui/legible </dev/null \
		>"$work/$label.log" 2>&1; then
		ok "$label"
	else
		fail "$label: $(tail -6 "$work/$label.log" | tr '\n' ' ')"
	fi
}

cat >"$work/read_in_full_test.go" <<'GO'
package legible_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/legible"
)

// TestThePseudoLocaleScannerReadsASplitDocumentWhole hands legible.Scan three documents whose
// text runs an ignored end tag splits, and asserts the text a person reads — collected in
// document order, joined — rather than a node count, so the assertion holds whichever way the
// parser grouped the runs. A character reference is resolved before it is compared, because that
// is what the scanner promises to hand back, and the run count is kept inside the number of nodes
// the document can be parsed into at all, so a scan that split a run into pieces would answer a
// number this range refuses.
func TestThePseudoLocaleScannerReadsASplitDocumentWhole(t *testing.T) {
	for _, table := range []struct {
		what     string
		body     string
		want     string
		maxNodes int
	}{
		{"one run split by an end tag that closes nothing",
			"<html><body><main>a</x>b</main></body></html>", "ab", 2},
		{"a split run that carries a character reference",
			"<html><body><main>a&amp;</x>b</main></body></html>", "a&b", 2},
		{"the shape a coalescing step is quadratic in",
			"<html><body><main>" + strings.Repeat("run</x>", 500) + "</main></body></html>",
			strings.Repeat("run", 500), 500},
	} {
		collected, err := legible.Scan([]byte(table.body), nil)
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
		if got := strings.Join(texts, ""); got != table.want {
			t.Errorf("the scan of %s hands back the text %q, want %q", table.what, shorten(got), shorten(table.want))
		}
		if len(texts) < 1 || len(texts) > table.maxNodes {
			t.Errorf("the scan of %s collected %d text node(s), of the %d that document can be parsed into",
				table.what, len(texts), table.maxNodes)
		}
	}
}

// shorten keeps a failure readable: the third document holds 1 500 characters, printed twice.
func shorten(text string) string {
	if len(text) <= 40 {
		return text
	}
	return text[:20] + " … " + text[len(text)-20:]
}
GO

overlay_of "$work/shipped.json"
ask "the scanner reads every run of a document it did not write" "$work/shipped.json" "$scan_case"

# The same case of a scanner that stops reading one element's text: unless this copy is refused, the
# `ok` above says only that the shipped scanner agrees with itself.
if ! python3 - ui/legible/legible.go "$work/quiet.go" <<'PY'
import sys
src, out = sys.argv[1], sys.argv[2]
needle = """		case html.TextNode:
			if canon(node.Data) != "" {"""
text = open(src).read()
if text.count(needle) != 1:
    sys.exit("%s: needle found %d time(s), want 1" % (src, text.count(needle)))
open(out, "w").write(text.replace(needle, needle.replace('!= "" {', '!= "" && !strings.Contains(path, "main") {')))
PY
then
	fail "the quiet-scanner copy could not be written, so the case below asked nothing"
else
	overlay_of "$work/quiet.json" "ui/legible/legible.go=$work/quiet.go"
	if [ -f "$work/quiet.json" ] && go test -overlay "$work/quiet.json" -count=1 -run "^${scan_case}\$" \
		./ui/legible </dev/null >"$work/quiet.log" 2>&1; then
		fail "the case passes on a scanner that reads no text under <main>, so it answers to nothing"
	else
		ok "the same case refuses a scanner that reads no text under <main>"
	fi
fi

[ "$failures" -eq 0 ] || { echo "$failures case(s) failed"; exit 1; }
echo 'the pseudo-locale scanner reads a split document whole, and a scanner that reads less is refused: all cases hold'
