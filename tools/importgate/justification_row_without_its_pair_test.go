package main

import (
	"strings"
	"testing"
)

// TestJustificationRowNamingNoProductionPairIsRefused checks the whole of the
// `module-layer-cycle-unused` promise: a `<->` row is the one exception a layer
// declaration may carry, so a row whose pair holds no reciprocal production
// import is refused whether the pair still imports one way or holds no
// production edge between its two modules at all. The row is refused the way an
// unused contract baseline row is, because a declaration that can keep an
// exception nobody measured can grow instead of shrinking.
func TestJustificationRowNamingNoProductionPairIsRefused(t *testing.T) {
	const assigned = product + "/modules/a platform\n" + product + "/modules/b platform\n"
	const row = product + "/modules/a <-> " + product + "/modules/b Both directions moved behind declared ports.\n"

	t.Run("the pair holds no edge at all", func(t *testing.T) {
		g := fixtureGraph()
		g.owned = []string{product + "/modules/a", product + "/modules/b"}
		got := decide(g, nil, nil, layersFixture(t, g, product, assigned+row)).refused
		if len(got) != 1 || got[0].rule != "module-layer-cycle-unused" {
			t.Fatalf("a row for a pair with no production edge was accepted: %+v", got)
		}
	})

	t.Run("the pair holds test edges only", func(t *testing.T) {
		g := fixtureGraph(
			edge{product + "/modules/a_test", product + "/modules/b/contracts", product + "/modules/a", "external-test", "modules/a@external"},
			edge{product + "/modules/b_test", product + "/modules/a/contracts", product + "/modules/b", "external-test", "modules/b@external"},
		)
		got := decide(g, nil, nil, layersFixture(t, g, product, assigned+row)).refused
		if len(got) != 1 || got[0].rule != "module-layer-cycle-unused" {
			t.Fatalf("a row for a pair that is reciprocal in test source only was accepted: %+v", got)
		}
	})

	t.Run("a refusal names the row and its reason", func(t *testing.T) {
		g := fixtureGraph()
		g.owned = []string{product + "/modules/a", product + "/modules/b"}
		got := decide(g, nil, nil, layersFixture(t, g, product, assigned+row)).refused
		if text := diagnostics(got); !strings.Contains(text, "module-layer-cycle-unused") ||
			!strings.Contains(text, product+"/modules/a") || !strings.Contains(text, "declared ports") {
			t.Fatalf("diagnostic lost the row: %s", text)
		}
	})

}
