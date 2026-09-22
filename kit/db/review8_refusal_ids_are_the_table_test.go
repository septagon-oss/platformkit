package db_test

// review8_refusal_ids_are_the_table_test.go is the ninth review's pin for the sentence
// kit/db/README.md and kit/db/refusal_names_test.go both write about themselves:
//
//	"an id named here that nothing prints, or a printed id nothing names, fails that case
//	 rather than drifting."
//
// Neither half is true of `refusal_names_test.go` as it stands. Its README leg walks a
// literal list of the four ids, and its message leg refuses one file per id from that same
// list, so the four that are written down are pinned in both directions and the *set* is
// pinned in neither. Measured in a copy of HEAD:
//
//   - a fifth row added to README's table — `drain-has-no-window`, an id no code prints —
//     and `go test ./kit/db ./migrations ./kit/app` is green;
//   - a fifth id added to kit/db/refusals.go and printed by the drain's two-statement
//     refusal, named by no document anywhere, and the same packages are green.
//
// Renaming one of the four, in either file, does fail — so the gate is real for what it
// lists and silent about everything else, which is the drift both sentences promise
// against. Listing the ids twice, once in Go and once in Markdown, is what makes the set
// checkable only by a reader who reads both; this case reads both, and asks that they hold
// the same names.
//
// The ids come out of `refusals.go`, the file whose own comment says the id is printed
// "because a runbook, a log line or a review that names one has to lead somewhere in the
// repository that refused it". A name in that file and not in the operator's table — or in
// the table and not in that file — is the pair coming apart, which is what the leg below
// refuses in both directions.

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// refusalIDConst is one line of kit/db/refusals.go's const block: an id, in the words the
// runner prints, next to the name the code calls it by.
var refusalIDConst = regexp.MustCompile(`(?m)^\s*(refusal[A-Za-z]*)\s*=\s*"([a-z0-9-]+)"\s*$`)

// refusalTableID is one row of README's refusal table — the operator's copy of the same list.
var refusalTableID = regexp.MustCompile(`(?m)^\| ` + "`" + `([a-z0-9-]+)` + "`" + ` \|`)

func TestTheRefusalIDsTheCodeDeclaresAreTheOnesItsTableLists(t *testing.T) {
	source, err := os.ReadFile("refusals.go")
	if err != nil {
		t.Fatalf("kit/db/refusals.go, the file that names the ids: %v", err)
	}
	declared := map[string]bool{}
	for _, found := range refusalIDConst.FindAllStringSubmatch(string(source), -1) {
		declared[found[2]] = true
	}
	if len(declared) == 0 {
		t.Fatal("refusals.go declares no refusal id at all, which is not what its comment says")
	}
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("this package's own README.md: %v", err)
	}
	tabled := map[string]bool{}
	for _, found := range refusalTableID.FindAllStringSubmatch(string(readme), -1) {
		tabled[found[1]] = true
	}
	if len(tabled) == 0 {
		t.Fatal("README.md tables no refusal id at all, which is not what the paragraph above its table says")
	}
	for id := range declared {
		if !tabled[id] {
			t.Errorf("refusals.go prints %q and README.md never names it: an operator who reaches for the table after a log line finds no row, and the two lists have started to be different lists", id)
		}
	}
	for id := range tabled {
		if !declared[id] {
			t.Errorf("README.md names %q and nothing in refusals.go prints it: the table promises a run that answers with a name no code says", id)
		}
	}
	// The four this review's predecessor found are named here so that a change which
	// quietly empties both lists — one constant and one table row at a time — still fails.
	for _, id := range []string{
		"data-table-missing", "data-key-not-primary-key",
		"contract-without-expansion", "backfill-exceeds-install-budget",
	} {
		if !declared[id] || !tabled[id] {
			t.Errorf("%q is in neither list any more: it is the pair a runbook quote leads from a log line to this repository", id)
		}
	}
	if strings.Count(string(readme), "refusal <id>") == 0 {
		t.Error("README.md no longer states the shape a refusal prints, which is the thing an operator greps for")
	}
}
