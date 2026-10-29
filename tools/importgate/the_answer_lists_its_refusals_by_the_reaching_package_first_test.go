package main

import (
	"strings"
	"testing"
)

// TestTheAnswerListsItsRefusalsByTheReachingPackageFirst reads the sequence an
// answer that refuses several imports prints: decide sorts its refusals by the
// package that reached, then by what it reached, then by the kind of source and the
// rule, so a person fixing one module reads that module's refusals together and one
// tree answers the same way twice. The fixture makes the reaching package sort first
// while the target it reaches sorts last, so an answer sorted by the target instead
// prints these same lines in another order. Inputs: one fixture graph whose contract
// package reaches a foreign internal from production source and from a test file, and
// two contract baseline rows whose import no source carries. Failure: the lines in
// another sequence, or a second run of the same graph printing another answer.
func TestTheAnswerListsItsRefusalsByTheReachingPackageFirst(t *testing.T) {
	rows, err := parseBaseline("baseline",
		"modules/a/contracts testing The frozen signature names a value type of this package.\n"+
			"modules/z/contracts net/http/httptest The frozen handler answers through a recorder.", fixtureGraph())
	if err != nil {
		t.Fatalf("baseline rows: %v", err)
	}
	reaching, reached := product+"/modules/a/contracts", product+"/modules/b/internal"
	g := fixtureGraph(
		edge{reaching, reached, reaching, "production", ""},
		edge{reaching, reached, reaching, "internal-test", ""},
	)
	line := func(rule, from, to, context string) string {
		return "OUT OF BOUNDS [" + rule + "] " + from + " -> " + to + " (" + context + "): " + remedies[rule]
	}
	want := strings.Join([]string{
		line("foreign-internal", reaching, reached, "internal-test"),
		line("contract-adapter", reaching, reached, "production"),
		line("foreign-internal", reaching, reached, "production"),
		line("contract-baseline-unused", reaching, "testing", "baseline") +
			"; row reason: The frozen signature names a value type of this package.",
		line("contract-baseline-unused", product+"/modules/z/contracts", "net/http/httptest", "baseline") +
			"; row reason: The frozen handler answers through a recorder.",
	}, "\n") + "\n"
	answer := diagnostics(decideFixture(g, nil, rows).refused)
	if answer != want {
		t.Errorf("the answer must list its refusals by the package that reached, want %q printed %q", want, answer)
	}
	if again := diagnostics(decideFixture(g, nil, rows).refused); again != answer {
		t.Errorf("the same graph must refuse the same way twice, first %q then %q", answer, again)
	}
}
