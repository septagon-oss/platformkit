package main

import (
	"strings"
	"testing"
)

// pairNotice returns the state one notice line gives a pair row, between the pair
// it names and the reason the row carries.
func pairNotice(t *testing.T, notice, pair, reason string) (state string) {
	t.Helper()
	prefix, suffix := "imports: module layer pair "+pair+"; ", "; "+reason
	lines, found := strings.Split(notice, "\n"), 0
	for _, line := range lines {
		if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, suffix) {
			continue
		}
		state, found = strings.TrimSuffix(strings.TrimPrefix(line, prefix), suffix), found+1
	}
	if found != 1 {
		t.Fatalf("notice announces %s %d times, want once: %s", pair, found, notice)
	}
	return state
}

// TestJustificationRowIsJudgedOnceByTheBuildThatHoldsBothModules checks that a
// pair row has exactly one judge, and which build that is. Go reports packages for
// the main module alone, so a consumer can never mark a pair of another
// repository's modules justified: it announces those rows and leaves them to the
// build that holds both modules. That build must still refuse a row whose pair
// holds no production edge in either direction, or the one exception a declaration
// may carry would be free to grow in whichever build never looks at it.
func TestJustificationRowIsJudgedOnceByTheBuildThatHoldsBothModules(t *testing.T) {
	const (
		b, c, e = "modules/b", "modules/c", "modules/e"
		genuine = "Both contract trees name each other's values."
		stale   = "An old reason nobody moved."
	)
	assigned := dependency + "/" + b + " platform\n" + dependency + "/" + c + " platform\n" + dependency + "/" + e + " platform\n"
	row := func(one, two, reason string) string {
		return dependency + "/" + one + " <-> " + dependency + "/" + two + " " + reason + "\n"
	}
	declared := dependency + "/" + b + " <-> " + dependency + "/" + c
	unmeasured := dependency + "/" + b + " <-> " + dependency + "/" + e
	declaredRows := row(b, c, genuine) + row(b, e, stale)

	// The consumer sees no package of the dependency at all, so neither row is
	// measurable there and neither is judged there: an announcement is all it owes.
	t.Run("a consumer announces a pair it cannot measure", func(t *testing.T) {
		g := fixtureGraph(edge{product + "/" + b + "/contracts", dependency + "/" + b + "/contracts",
			product + "/" + b + "/contracts", "production", ""})
		g.owned = []string{product + "/" + b}
		into := newLayerDeclaration()
		for _, pass := range []struct{ file, rootPath, text string }{
			{"scripts/imports-layers.txt", product, product + "/" + b + " platform\n"},
			{"foundation/scripts/imports-layers.txt", dependency, assigned + declaredRows},
		} {
			if err := parseLayers(pass.file, pass.text, g, pass.rootPath, into); err != nil {
				t.Fatalf("%s: %v", pass.file, err)
			}
		}
		got := decide(g, nil, nil, into)
		if len(got.refused) != 0 {
			t.Fatalf("a consumer judged the repository that holds the pair: %+v", got.refused)
		}
		notice := into.notices(got.spent, got.justified)
		for _, row := range []struct{ pair, reason string }{{declared, genuine}, {unmeasured, stale}} {
			if state := pairNotice(t, notice, row.pair, row.reason); state !=
				"not measured in this build context; "+dependency+" judges this row in its own build" {
				t.Errorf("pair %s reads %q over a build that holds neither module", row.pair, state)
			}
		}
	})

	// The build that holds both modules judges both rows: the reciprocal pair is
	// accepted, the pair that stopped importing each other is refused by name, and
	// neither line is announced as another repository's verdict.
	t.Run("the build that holds both modules judges each row", func(t *testing.T) {
		g := fixtureGraph(
			edge{dependency + "/" + b + "/internal", dependency + "/" + c + "/contracts",
				dependency + "/" + b + "/internal", "production", ""},
			edge{dependency + "/" + c + "/contracts", dependency + "/" + b + "/contracts",
				dependency + "/" + c + "/contracts", "production", ""})
		g.self, g.dir = dependency, "/fixture/foundation"
		g.owned = []string{dependency + "/" + b, dependency + "/" + c, dependency + "/" + e}
		into := layersFixture(t, g, dependency, assigned+declaredRows)
		got := decide(g, nil, nil, into)
		if len(got.refused) != 1 || got.refused[0].rule != "module-layer-cycle-unused" ||
			got.refused[0].from != dependency+"/"+b || got.refused[0].to != dependency+"/"+e {
			t.Fatalf("the owning build judged the rows %+v, want one refusal of the pair that holds no edge", got.refused)
		}
		notice := into.notices(got.spent, got.justified)
		if state := pairNotice(t, notice, declared, genuine); state != "reciprocal in production in this build context" {
			t.Errorf("the reciprocal pair reads %q where both directions are measured", state)
		}
		if state := pairNotice(t, notice, unmeasured, stale); state != "not reciprocal in production in this build context" {
			t.Errorf("the pair with no edge reads %q where both directions could be measured", state)
		}
	})
}
