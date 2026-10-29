package main

import (
	"strings"
	"testing"
)

// TestEveryPrintedRefusalCarriesTheAdviceThatNamesTheWayOut renders every rule this
// package can refuse with through diagnostics, the only place a refusal becomes the
// line a person reads, and requires that line to carry the rule, both packages of the
// pair and the non-empty advice the remedy map holds for it. The pairing of rule to
// advice can be checked in the map alone and still fail the reader: a rule reaches a
// person only as one printed line, so this walks the printed line itself. Inputs: the
// rule names this package's source can hand to a refusal, and one synthetic pair per
// rule, since the advice depends on the rule and on nothing else. Failure: a printed
// refusal with no advice after the pair, an advice that differs from the map's, an
// empty advice, or a line that lost one of the two packages.
func TestEveryPrintedRefusalCarriesTheAdviceThatNamesTheWayOut(t *testing.T) {
	owner := foundation + "/modules/billing/contracts"
	target := foundation + "/modules/ledger/contracts"
	for _, rule := range packageRuleNames(t) {
		printed := diagnostics([]refusal{{
			edge: edge{from: owner, to: target, ownerPath: owner, context: "production"},
			rule: rule,
		}})
		line, rest, one := strings.Cut(printed, "\n")
		if !one || rest != "" {
			t.Errorf("the refusal of %q must print exactly one line, printed %q", rule, printed)
			continue
		}
		head, advice, separated := strings.Cut(line, "): ")
		if !separated {
			t.Errorf("the refusal of %q carries nothing after the pair for a reader to act on: %q", rule, line)
			continue
		}
		if want := remedies[rule]; advice != want || strings.TrimSpace(want) == "" {
			t.Errorf("the refusal of %q must print the remedy %q beside the pair, printed %q", rule, want, advice)
		}
		if !strings.HasPrefix(head, "OUT OF BOUNDS ["+rule+"] ") {
			t.Errorf("the refusal does not name the rule it refused: %q", line)
		}
		if !strings.Contains(head, owner) || !strings.Contains(head, target) {
			t.Errorf("the refusal of %q does not name both packages of the pair: %q", rule, line)
		}
	}
}
