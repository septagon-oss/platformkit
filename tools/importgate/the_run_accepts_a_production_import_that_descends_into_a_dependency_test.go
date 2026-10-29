package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// TestTheRunAcceptsAProductionImportThatDescendsIntoADependency checks the order's
// other answer over two real repositories: the checked repository ranks its own
// module product and a declared dependency ranks its own module generic, so a
// production import from this repository's contracts tree into that dependency's
// contracts tree is what the order admits — status 0, the accepted line, nothing
// refused — and `--modules` over the same two trees ranks the foreign node, and the
// edge that reaches it, by the layer that dependency declared for its own module.
// The run assembles that one declaration for both answers, so a run that handed its
// verdict this repository's rows while keeping the merged ones for its document
// refuses a composition the order allows here with module-layer-missing, and one
// that did the reverse prints the foreign node as unassigned with no direction
// computed for the edge.
func TestTheRunAcceptsAProductionImportThatDescendsIntoADependency(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-buildvcs=false")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
	root, dependencyDir := t.TempDir(), t.TempDir()
	writeFixture(t, dependencyDir, "go.mod", "module "+dependency+"\n\ngo 1.26.6\n")
	writeFixture(t, dependencyDir, "modules/b/contracts/values.go", "package contracts\n")
	writeFixture(t, dependencyDir, layerFile, dependency+"/modules/b generic\n")
	writeFixture(t, root, "go.mod", fmt.Sprintf("module %s\n\ngo 1.26.6\nrequire %s v0.0.0\nreplace %s => %q\n",
		product, dependency, dependency, dependencyDir))
	writeFixture(t, root, "modules/a/contracts/values.go", "package contracts\nimport _ \""+dependency+"/modules/b/contracts\"\n")
	writeFixture(t, root, layerFile, product+"/modules/a product\n")
	const near, away = product + "/modules/a", dependency + "/modules/b"
	var stdout, stderr bytes.Buffer
	if code := run(t.Context(), []string{root, dependency}, &stdout, &stderr, runGo); code != 0 ||
		!strings.Contains(stdout.String(), "satisfy the module boundaries") ||
		strings.Contains(stderr.String(), "OUT OF BOUNDS") {
		t.Fatalf("an import that descends into a declared dependency: status %d, stdout %s, stderr %s, want 0 accepted with nothing refused", code, &stdout, &stderr)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run(t.Context(), []string{"--modules", root, dependency}, &stdout, &stderr, runGo); code != 0 {
		t.Fatalf("report mode over two readable declarations: status %d, stderr %s, want 0 with a report", code, &stderr)
	}
	var report moduleReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("the report is not readable: %v; %s", err, &stdout)
	}
	ranked, computed := moduleNode{}, ""
	for _, node := range report.Nodes {
		if node.Module == away {
			ranked = node
		}
	}
	for _, edge := range slices.Concat(report.Edges, report.Constructions) {
		if edge.From == near && edge.To == away {
			computed = edge.Direction
		}
	}
	if ranked.Layer != "generic" || ranked.Repository != dependency {
		t.Errorf("node %s reads layer %q of repository %q, want layer generic of %s; %s", away, ranked.Layer, ranked.Repository, dependency, &stdout)
	}
	if computed != directionDown {
		t.Errorf("the aggregate edge %s -> %s computes direction %q, want down, which only the two declared layers give; %s", near, away, computed, &stdout)
	}
}
