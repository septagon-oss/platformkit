package main

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// TestTheModuleDocumentJustifiesThePairWithItsRowAndBothEdges checks the
// justification column over one tree that fails both ways at once: a reciprocal pair
// no row declares, a row whose pair holds one production direction only, and a
// reciprocal pair that carries its row and reason. `--modules` answers the first two
// with status 0 and the document anyway, so the document is where a reader meets the
// difference. Only the pair a map declares and this build measures in both directions
// may read justified, and with its own reason: a column built from what this run
// measured prints the undeclared pair as an exception no declaration holds, one built
// from the rows alone prints the stale row's pair as one, and a reason read off the
// wrong pair lands an exception on an edge that never carried it.
func TestTheModuleDocumentJustifiesThePairWithItsRowAndBothEdges(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-buildvcs=false")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
	const (
		kept  = "Both contract trees name each other's values, and neither direction moves without reshaping a published surface."
		stale = "An old reason nobody moved behind a port."
	)
	root := t.TempDir()
	writeFixture(t, root, "go.mod", "module "+product+"\n\ngo 1.26.6\n")
	// Two directions with no row, one direction with a row, and two directions with
	// its row — three pairs, so one wrong row is visible as the wrong pair justified.
	for _, pair := range []struct {
		near, away string
		reciprocal bool
	}{
		{"a", "b", true}, {"c", "d", false}, {"e", "f", true},
	} {
		writeFixture(t, root, "modules/"+pair.near+"/contracts/values.go", "package contracts\nimport _ \""+product+"/modules/"+pair.away+"/contracts\"\n")
		writeFixture(t, root, "modules/"+pair.away+"/contracts/values.go", "package contracts\n")
		if pair.reciprocal {
			writeFixture(t, root, "modules/"+pair.away+"/"+pair.away+".go", "package "+pair.away+"\nimport _ \""+product+"/modules/"+pair.near+"/contracts\"\n")
		}
	}
	writeFixture(t, root, layerFile, product+"/modules/a identity\n"+product+"/modules/b identity\n"+
		product+"/modules/c identity\n"+product+"/modules/d identity\n"+
		product+"/modules/e identity\n"+product+"/modules/f identity\n"+
		product+"/modules/c <-> "+product+"/modules/d "+stale+"\n"+
		product+"/modules/e <-> "+product+"/modules/f "+kept+"\n")
	var stdout, stderr bytes.Buffer
	if code := run(t.Context(), []string{root}, &stdout, &stderr, runGo); code != 1 ||
		!strings.Contains(stderr.String(), "module-layer-cycle-undeclared") ||
		!strings.Contains(stderr.String(), "module-layer-cycle-unused") {
		t.Fatalf("a tree that fails both ways: status %d, stdout %s, stderr %s, want 1 refused as an undeclared cycle and an unused row",
			code, &stdout, &stderr)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run(t.Context(), []string{"--modules", root}, &stdout, &stderr, runGo); code != 0 ||
		!strings.Contains(stderr.String(), "module-layer-cycle-undeclared") ||
		!strings.Contains(stderr.String(), "module-layer-cycle-unused") {
		t.Fatalf("the module document over that tree: status %d, stdout %s, stderr %s, want 0 with both refusals named",
			code, &stdout, &stderr)
	}
	var report moduleReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("the report is not readable: %v; %s", err, &stdout)
	}
	own := func(module string) string { return product + "/modules/" + module }
	undeclared := [2]string{own("a"), own("b")}
	staleRow := [2]string{own("c"), own("d")}
	accepted := [2]string{own("e"), own("f")}
	both := func(pair [2]string) [][2]string { return [][2]string{pair, {pair[1], pair[0]}} }
	for _, pair := range [][2]string{undeclared, {undeclared[1], undeclared[0]}, staleRow} {
		if edge := aggregateEdge(report, pair[0], pair[1]); len(edge.Packages) != 1 {
			t.Fatalf("the document holds no %s -> %s edge to judge: %+v; %s", pair[0], pair[1], edge, &stdout)
		}
	}
	for _, pair := range both(accepted) {
		if edge := aggregateEdge(report, pair[0], pair[1]); !edge.Justified || edge.Reason != kept {
			t.Errorf("the document prints %s -> %s as justified %v with reason %q, want justified with %q",
				pair[0], pair[1], edge.Justified, edge.Reason, kept)
		}
	}
	for _, pair := range append(both(undeclared), both(staleRow)...) {
		if edge := aggregateEdge(report, pair[0], pair[1]); edge.Justified || edge.Reason != "" {
			t.Errorf("the document prints %s -> %s as justified %v with reason %q, want an edge this build gives no exception to",
				pair[0], pair[1], edge.Justified, edge.Reason)
		}
	}
	for _, edge := range slices.Concat(report.Edges, report.Constructions) {
		belongs := edge.From == accepted[0] || edge.From == accepted[1]
		belongs = belongs && (edge.To == accepted[0] || edge.To == accepted[1])
		if edge.Reason == kept && !belongs {
			t.Errorf("the document carries %q on %s -> %s, a reason only %s and %s's row holds; %s",
				kept, edge.From, edge.To, accepted[0], accepted[1], &stdout)
		}
	}
}
