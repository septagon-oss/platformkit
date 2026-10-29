package main

import "testing"

// TestEachPrintedRefusalCarriesTheAdviceAndRowReasonOfItsOwnLine reads the advice and the
// row reason through one answer that refuses several imports at once. A refusal's rule, the
// advice the remedy map holds for that rule and the reason of the row that refusal came from
// are three facts about that refusal, and diagnostics prints them beside that refusal's own
// pair: an advice or a reason chosen once for the answer — from its first refusal, or from
// any other — still prints a real remedy on every line while telling the reader to fix a pair
// no refused row asked to be fixed. Inputs: one call over a stale pair row carrying its own
// reason, an unused baseline row carrying another and an undeclared pair carrying none, in
// both orders, each line carrying the advice of its own rule. Failure: a line advised for
// another refusal's rule, a reason belonging to another row, or a line that lost both.
func TestEachPrintedRefusalCarriesTheAdviceAndRowReasonOfItsOwnLine(t *testing.T) {
	const staleRule, carriedRule, undeclaredRule = "module-layer-cycle-unused", "contract-baseline-unused", "module-layer-cycle-undeclared"
	a, b, pkg := product+"/modules/a", product+"/modules/b", product+"/modules/a/contracts"
	staleRow := "OUT OF BOUNDS [" + staleRule + "] " + a + " -> " + b + " (layer): " + remedies[staleRule] + "; row reason: A row names a pair this build no longer measures.\n"
	carriedRow := "OUT OF BOUNDS [" + carriedRule + "] " + pkg + " -> " + b + "/contracts (baseline): " + remedies[carriedRule] + "; row reason: A baseline row carries an edge this build no longer sees.\n"
	undeclared := "OUT OF BOUNDS [" + undeclaredRule + "] " + b + " -> " + a + " (production): " + remedies[undeclaredRule] + "\n"
	stale := refusal{edge: edge{a, b, a, "layer", ""}, rule: staleRule, detail: "A row names a pair this build no longer measures."}
	carried := refusal{edge: edge{pkg, b + "/contracts", pkg, "baseline", ""}, rule: carriedRule, detail: "A baseline row carries an edge this build no longer sees."}
	open := refusal{edge: edge{b, a, b, "production", ""}, rule: undeclaredRule}
	for _, tc := range []struct {
		name    string
		refused []refusal
		want    string
	}{
		{"stale row first", []refusal{stale, carried, open}, staleRow + carriedRow + undeclared},
		{"undeclared pair first", []refusal{open, carried, stale}, undeclared + carriedRow + staleRow},
	} {
		if printed := diagnostics(tc.refused); printed != tc.want {
			t.Errorf("%s must print exactly %q, printed %q", tc.name, tc.want, printed)
		}
	}
}
