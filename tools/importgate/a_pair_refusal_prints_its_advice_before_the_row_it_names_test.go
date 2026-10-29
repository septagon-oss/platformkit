package main

import (
	"strings"
	"testing"
)

// TestAPairRefusalPrintsItsAdviceBeforeTheRowItNames renders the two refusals the layer
// rules raise over a pair of modules — a pair that imports itself back with no row saying
// why, and a row whose pair no longer imports back — through diagnostics, the one place a
// refusal becomes the line a person reads. It requires the advice the remedy map holds for
// the rule to print directly after the pair, and a stale row's own reason to print after
// that advice rather than in its place. Inputs: two fixture graphs of one repository over
// one layer map whose rows keep both modules in one layer, so a pair is the only thing
// refused. The pairing reads this package carries each print a rule over a hand-made pair
// in the production context with no row reason, so the pair refusals, the only ones that
// print in the layer context and the only ones that carry a row's reason, are printed as
// lines by nothing else here. Failure: a refused pair that prints no advice after it, an
// advice that is not the map's, an advice pushed behind the reason, or a stale row whose
// refusal names no reason for the row it asks the reader to remove.
func TestAPairRefusalPrintsItsAdviceBeforeTheRowItNames(t *testing.T) {
	a, b := product+"/modules/a", product+"/modules/b"
	one, two := product+"/modules/a/contracts", product+"/modules/b/contracts"
	const reason = "Both contract trees name each other's values by design."
	rows := a + " platform\n" + b + " platform\n"
	for _, tc := range []struct {
		rule   string
		reason string
		edges  []edge
		text   string
	}{
		{
			rule:  "module-layer-cycle-undeclared",
			edges: []edge{{one, two, one, "production", ""}, {two, one, two, "production", ""}},
			text:  rows,
		},
		{
			rule:   "module-layer-cycle-unused",
			reason: reason,
			edges:  []edge{{one, two, one, "production", ""}},
			text:   rows + a + " <-> " + b + " " + reason + "\n",
		},
	} {
		g := fixtureGraph(tc.edges...)
		declared := layersFixture(t, g, product, tc.text)
		refused := decide(g, nil, nil, declared).refused
		var pair *refusal
		for at, r := range refused {
			if r.rule == tc.rule {
				pair = &refused[at]
			}
		}
		if pair == nil {
			t.Fatalf("the layer rules refused no %q over %+v, refused %+v", tc.rule, tc.edges, refused)
		}
		line := diagnostics([]refusal{*pair})
		if !strings.Contains(line, "): "+remedies[tc.rule]) {
			t.Errorf("the refusal of %q prints no advice for a reader to act on beside the pair: %q", tc.rule, line)
		}
		if tc.reason != "" && !strings.Contains(line, "; row reason: "+tc.reason) {
			t.Errorf("the refusal of a stale pair row prints no reason for the row it asks to be removed: %q", line)
		}
	}
}
