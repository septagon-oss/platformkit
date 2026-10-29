package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestTheModuleReportCarriesTheLayerADependencyDeclares checks that `--modules`
// ranks the modules of a declared dependency by that dependency's own declaration
// rather than only the modules of the repository the run checks. This is the mode a
// machine consumes: scripts/module_graph.sh renders docs/MODULE-GRAPH.md from it and
// the package README composes it with a dependency root, and that document's Modules
// table and the direction of every edge row come out of these rows. A report that
// ranked only its own repository still prints a graph at status 0, so its reader
// would render every foreign module as unassigned with no refusal anywhere to tell it
// that a read declaration was never consulted.
func TestTheModuleReportCarriesTheLayerADependencyDeclares(t *testing.T) {
	root := t.TempDir()
	foreign := filepath.Join(root, "foundation")
	writeFixture(t, root, layerFile, product+"/modules/a product\n")
	writeFixture(t, foreign, layerFile, dependency+"/modules/b generic\n")
	p := packageFixture("modules/a")
	p.Dir, p.Module.Dir = filepath.Join(root, "modules/a"), root
	p.Imports = []string{dependency + "/modules/b/contracts"}
	base := fakeCommand(t, root, jsonStream(t, p))
	call := func(ctx context.Context, directory string, args ...string) ([]byte, error) {
		if strings.Join(args, " ") == "list -m -json "+dependency {
			return jsonStream(t, module{Path: dependency, Dir: foreign}), nil
		}
		return base(ctx, directory, args...)
	}
	var stdout, stderr bytes.Buffer
	if status := run(t.Context(), []string{"--modules", root, dependency}, &stdout, &stderr, call); status != 0 {
		t.Fatalf("two readable declarations: status %d, stderr %s, want 0 with a report", status, &stderr)
	}
	var report moduleReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("the report is not readable: %v; %s", err, &stdout)
	}
	nodes := map[string]moduleNode{}
	for _, node := range report.Nodes {
		nodes[node.Module] = node
	}
	const near, away = product + "/modules/a", dependency + "/modules/b"
	if node := nodes[away]; node.Layer != "generic" || node.Repository != dependency {
		t.Errorf("node %s reads layer %q of repository %q, want layer generic of %s; %s", away, node.Layer, node.Repository, dependency, &stdout)
	}
	if node := nodes[near]; node.Layer != "product" {
		t.Errorf("node %s reads layer %q, want product; %s", near, node.Layer, &stdout)
	}
	computed := ""
	for _, edge := range slices.Concat(report.Edges, report.Constructions) {
		if edge.From == near && edge.To == away {
			computed = edge.Direction
		}
	}
	if computed != directionDown {
		t.Errorf("the aggregate edge %s -> %s computes direction %q, want down, which only two known layers give; %s", near, away, computed, &stdout)
	}
}
