package main

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// TestTheModuleReportCarriesTheJustificationThisRunSpent checks the justification
// half of the handoff the run performs: one reading feeds the verdict and the
// `--modules` document. Two modules of the checked repository that reach each other
// in production are one unit of composition, so the run refuses them until the map
// carries their row and a reason and accepts them once it does. Over that accepted
// tree `--modules` carries the row, with its reason, on both directions of the pair,
// because the document is given the justifications the same run spent. A run that
// hands it an empty set prints a pair the order allows as though nothing justified
// it, a document contradicting the accepted answer the same process just returned.
func TestTheModuleReportCarriesTheJustificationThisRunSpent(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-buildvcs=false")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
	const reason = "Both contract trees name each other's values."
	root := t.TempDir()
	// One module's contract tree names the other's values, and that module's own root
	// package reads back the first: two production directions between two modules, and
	// no Go package cycle.
	writeFixture(t, root, "go.mod", "module "+product+"\n\ngo 1.26.6\n")
	writeFixture(t, root, "modules/a/contracts/values.go", "package contracts\nimport _ \""+product+"/modules/b/contracts\"\n")
	writeFixture(t, root, "modules/b/contracts/values.go", "package contracts\n")
	writeFixture(t, root, "modules/b/b.go", "package b\nimport _ \""+product+"/modules/a/contracts\"\n")
	var stdout, stderr bytes.Buffer
	if code := run(t.Context(), []string{root}, &stdout, &stderr, runGo); code != 1 ||
		!strings.Contains(stderr.String(), "module-layer-cycle-undeclared") {
		t.Fatalf("a reciprocal pair with no row: status %d, stdout %s, stderr %s, want 1 refused as an undeclared cycle", code, &stdout, &stderr)
	}
	writeFixture(t, root, layerFile, product+"/modules/a identity\n"+product+"/modules/b identity\n"+
		product+"/modules/a <-> "+product+"/modules/b "+reason+"\n")
	stdout.Reset()
	stderr.Reset()
	if code := run(t.Context(), []string{root}, &stdout, &stderr, runGo); code != 0 ||
		!strings.Contains(stdout.String(), "satisfy the module boundaries") {
		t.Fatalf("a justified reciprocal pair: status %d, stdout %s, stderr %s, want 0 accepted", code, &stdout, &stderr)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run(t.Context(), []string{"--modules", root}, &stdout, &stderr, runGo); code != 0 {
		t.Fatalf("report mode over a justified pair: status %d, stderr %s, want 0 with a report", code, &stderr)
	}
	var report moduleReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("the report is not readable: %v; %s", err, &stdout)
	}
	own, other := product+"/modules/a", product+"/modules/b"
	for _, pair := range [][2]string{{own, other}, {other, own}} {
		if edge := aggregateEdge(report, pair[0], pair[1]); !edge.Justified || edge.Reason != reason {
			t.Errorf("the aggregate edge %s -> %s reads justified %v with reason %q, want justified with %q; %s",
				pair[0], pair[1], edge.Justified, edge.Reason, reason, &stdout)
		}
	}
}

// aggregateEdge finds one direction of the aggregate graph, import edge or
// composition, so a case asks the pair rather than which list it landed in.
func aggregateEdge(report moduleReport, from, to string) moduleEdge {
	for _, edge := range slices.Concat(report.Edges, report.Constructions) {
		if edge.From == from && edge.To == to {
			return edge
		}
	}
	return moduleEdge{From: from, To: to}
}
