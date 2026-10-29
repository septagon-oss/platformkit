package main

import (
	"slices"
	"testing"
)

// TestTheModuleReportListsEachSourceKindOfAPairInOrder reads the `sources` list of the
// aggregate report `--modules` answers, the machine-readable form of the module graph.
// scripts/module_graph.sh prints that list as the Source cell of every row of
// docs/MODULE-GRAPH.md in the order it arrives — the contexts of an edge it sorts again
// itself, and the packages of an edge get no row at all — so this is the one list of the
// report whose order the generated document takes directly, and a list that moves between
// two runs of one tree is two documents of one tree. The list is in this order only
// because moduleGraph sorts the kinds it collected (tools/importgate/report.go:185).
// Inputs: one graph whose rows beneath the pair from z into a span three kinds of
// consuming package. Failure: a source list out of the order of the kinds it names, or
// one that names a kind the fixture never wrote.
func TestTheModuleReportListsEachSourceKindOfAPairInOrder(t *testing.T) {
	z, a := product+"/modules/z", product+"/modules/a"
	g := fixtureGraph(
		edge{z + "/internal/x", a + "/contracts", z + "/internal/x", "internal-test", ""},
		edge{z + "/impl", a + "/contracts", z + "/impl", "production", ""},
		edge{z + "/contracts/api", a + "/contracts", z + "/contracts/api", "production", ""},
	)
	report := moduleGraph(g, fixtureLayers(g), nil)
	if len(report.Edges) != 1 {
		t.Fatalf("three rows beneath one pair answer %d aggregate rows: %v", len(report.Edges), report.Edges)
	}
	kinds := report.Edges[0].Sources
	if !slices.IsSorted(kinds) || !slices.Equal(kinds, []string{"contracts", "implementation", "internal"}) {
		t.Errorf("the pair %s -> %s lists sources %v, want the kinds it spans named in the order of the kinds",
			report.Edges[0].From, report.Edges[0].To, kinds)
	}
}
