package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestTheModuleDocumentJustifiesNoCycleNoRowDeclares checks the declaring half of the
// justification column of `--modules`: the build measures both production directions
// between two modules, which makes them one unit of composition the order refuses
// until a map carries a row over the pair, and the document has to print both of those
// edges with no justification and no reason. A column filled from what this same run
// measured, rather than from a row some repository declared, states an exception no
// declaration holds — and `--modules` answers that refusal with status 0, so the
// generated graph stands as the only statement a reader of the map gets.
func TestTheModuleDocumentJustifiesNoCycleNoRowDeclares(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-buildvcs=false")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
	root := t.TempDir()
	// Two production directions and no row over the pair: a contracts tree that names
	// its peer's values, and that peer's own root package reading back.
	writeFixture(t, root, "go.mod", "module "+product+"\n\ngo 1.26.6\n")
	writeFixture(t, root, "modules/a/contracts/values.go", "package contracts\nimport _ \""+product+"/modules/b/contracts\"\n")
	writeFixture(t, root, "modules/b/contracts/values.go", "package contracts\n")
	writeFixture(t, root, "modules/b/b.go", "package b\nimport _ \""+product+"/modules/a/contracts\"\n")
	writeFixture(t, root, layerFile, product+"/modules/a identity\n"+product+"/modules/b identity\n")
	var stdout, stderr bytes.Buffer
	if code := run(t.Context(), []string{root}, &stdout, &stderr, runGo); code != 1 ||
		!strings.Contains(stderr.String(), "module-layer-cycle-undeclared") {
		t.Fatalf("a reciprocal pair with no row: status %d, stdout %s, stderr %s, want 1 refused as an undeclared cycle",
			code, &stdout, &stderr)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run(t.Context(), []string{"--modules", root}, &stdout, &stderr, runGo); code != 0 ||
		!strings.Contains(stderr.String(), "module-layer-cycle-undeclared") {
		t.Fatalf("the module document over an undeclared cycle: status %d, stdout %s, stderr %s, want 0 with the refusal named",
			code, &stdout, &stderr)
	}
	var report moduleReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("the report is not readable: %v; %s", err, &stdout)
	}
	own, other := product+"/modules/a", product+"/modules/b"
	for _, pair := range [][2]string{{own, other}, {other, own}} {
		edge := aggregateEdge(report, pair[0], pair[1])
		if len(edge.Packages) != 1 {
			t.Fatalf("the document holds no %s -> %s edge to judge: %+v; %s", pair[0], pair[1], edge, &stdout)
		}
		if edge.Justified || edge.Reason != "" {
			t.Errorf("the document prints %s -> %s as justified %v with reason %q, want an edge no row declares",
				pair[0], pair[1], edge.Justified, edge.Reason)
		}
	}
}
