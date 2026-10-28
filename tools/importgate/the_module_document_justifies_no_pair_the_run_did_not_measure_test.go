package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestTheModuleDocumentJustifiesNoPairTheRunDidNotMeasure checks the justification
// column through the command rather than through the functions behind it:
// scripts/module_graph.sh renders what `--modules` prints, that mode answers a policy
// refusal with status 0, and the generated document is all a reader of the graph
// sees. A `<->` row over a pair with one production direction is refused as unused
// while the surviving direction stays one edge of the aggregate graph, so over that
// tree the document carries the edge with no justification and no reason of its own.
// A report that builds the column from the rows the map declares, rather than from
// the verdict this same run reached, prints an exception the run refused, and a
// status of 0 leaves the document standing alone.
func TestTheModuleDocumentJustifiesNoPairTheRunDidNotMeasure(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-buildvcs=false")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
	const reason = "Both directions moved behind declared ports."
	root := t.TempDir()
	// One production direction only: a contracts tree that reads its peer and no way
	// back, so the row over the pair names a reciprocation this build cannot measure.
	writeFixture(t, root, "go.mod", "module "+product+"\n\ngo 1.26.6\n")
	writeFixture(t, root, "modules/a/contracts/values.go", "package contracts\nimport _ \""+product+"/modules/b/contracts\"\n")
	writeFixture(t, root, "modules/b/contracts/values.go", "package contracts\n")
	writeFixture(t, root, layerFile, product+"/modules/a identity\n"+product+"/modules/b identity\n"+
		product+"/modules/a <-> "+product+"/modules/b "+reason+"\n")
	var stdout, stderr bytes.Buffer
	if code := run(t.Context(), []string{root}, &stdout, &stderr, runGo); code != 1 ||
		!strings.Contains(stderr.String(), "module-layer-cycle-unused") {
		t.Fatalf("a row over a one-way pair: status %d, stdout %s, stderr %s, want 1 refused as an unused row",
			code, &stdout, &stderr)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run(t.Context(), []string{"--modules", root}, &stdout, &stderr, runGo); code != 0 ||
		!strings.Contains(stderr.String(), "module-layer-cycle-unused") {
		t.Fatalf("the module document over a refused row: status %d, stdout %s, stderr %s, want 0 with the refusal named",
			code, &stdout, &stderr)
	}
	var report moduleReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("the report is not readable: %v; %s", err, &stdout)
	}
	own, other := product+"/modules/a", product+"/modules/b"
	edge := aggregateEdge(report, own, other)
	if len(edge.Packages) != 1 {
		t.Fatalf("the document holds no %s -> %s edge to judge: %+v; %s", own, other, edge, &stdout)
	}
	if edge.Justified || edge.Reason != "" {
		t.Errorf("the document prints the refused row as justified %v with reason %q, want an unjustified edge naming no reason",
			edge.Justified, edge.Reason)
	}
}
