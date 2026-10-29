package main

import "testing"

// TestTheModuleReportLeavesARefusedPairRowUnjustified checks the report half of one
// reading: a `<->` row reaches the module document only when the run spent it. One
// production direction between two modules leaves an edge in the aggregate graph and
// a row the verdict refuses as unused, so the document has to carry that edge
// unjustified and with no reason of its own. A report that copies a row the same
// process refused into `justified` hands a reader of the generated graph an
// exception the gate declined, and a run that answers `--modules` with status 0
// despite the refusal leaves nothing else to contradict it.
func TestTheModuleReportLeavesARefusedPairRowUnjustified(t *testing.T) {
	const assigned = product + "/modules/a identity\n" + product + "/modules/b identity\n"
	const reason = "Both directions moved behind declared ports."
	own, other := product+"/modules/a", product+"/modules/b"
	// One direction only: the pair is still one edge of the aggregate graph, and the
	// row over it names a reciprocation this build cannot measure.
	pair := fixtureGraph(edge{own + "/contracts", other + "/contracts", own + "/contracts", "production", ""})
	declared := layersFixture(t, pair, product, assigned+own+" <-> "+other+" "+reason+"\n")
	got := decide(pair, nil, nil, declared)
	if len(got.refused) != 1 || got.refused[0].rule != "module-layer-cycle-unused" {
		t.Fatalf("a row over a one-way pair: %v, want one refusal of the unused-row rule", got.refused)
	}
	edge := aggregateEdge(moduleGraph(pair, declared, got.justified), own, other)
	if len(edge.Packages) != 1 {
		t.Fatalf("the aggregate graph holds no %s -> %s edge to judge: %+v", own, other, edge)
	}
	if edge.Justified || edge.Reason != "" {
		t.Errorf("the report carries the refused row as justified %v with reason %q, want an unjustified edge naming no reason",
			edge.Justified, edge.Reason)
	}
}
