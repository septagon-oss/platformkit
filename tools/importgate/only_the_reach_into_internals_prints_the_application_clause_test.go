package main

import "testing"

// TestOnlyTheReachIntoInternalsPrintsTheApplicationClause reads the rule half of the
// condition that puts "apps/ reaches into: " in front of a printed refusal. The clause
// says whose import the refusal blames and why the reach is refused, which belongs to
// the one rule that refuses a reach into another module's internals; every other rule
// an application package can be refused for prints the pair alone. Inputs: one refusal
// per rule, each from the same application package and printed through diagnostics
// with the advice its rule carries. Failure: the clause standing before a rule it does
// not describe, or gone from the reach into internals it does describe.
func TestOnlyTheReachIntoInternalsPrintsTheApplicationClause(t *testing.T) {
	source := foundation + "/apps/platformkit/composition"
	for _, tc := range []struct{ name, rule, context, target string }{
		{"reach into internals", "foreign-internal", "production", foundation + "/modules/ledger/internal/ledger"},
		{"construction of an implementation", "foreign-implementation", "production", foundation + "/modules/ledger"},
		{"helper in production source", "test-helper-in-production", "production", foundation + "/modules/ledger/contracts/ledgertest"},
		{"constructor with no grant row", "undeclared-integration", "external-test", foundation + "/modules/ledger"},
	} {
		printed := diagnostics([]refusal{{
			edge: edge{from: source, to: tc.target, ownerPath: source, context: tc.context},
			rule: tc.rule,
		}})
		prefix := ""
		if tc.rule == "foreign-internal" {
			prefix = "apps/ reaches into: "
		}
		want := "OUT OF BOUNDS [" + tc.rule + "] " + prefix + source + " -> " + tc.target +
			" (" + tc.context + "): " + remedies[tc.rule] + "\n"
		if printed != want {
			t.Errorf("%s must print exactly %q, printed %q", tc.name, want, printed)
		}
	}
}
