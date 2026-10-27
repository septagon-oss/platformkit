package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDependencyLayerMapThisBuildCannotOpenRefusesTheModuleReport checks the
// aggregate report mode over a declared dependency's unopenable declaration, which
// is where an unread map would reach a machine rather than a reader:
// scripts/module_graph.sh renders docs/MODULE-GRAPH.md from `--modules`, and the
// README composes that mode with a dependency root. A reading failure answers as it
// answers in policy mode — status 2, that dependency's file named, nothing on
// stdout — because a report printed at status 0 over a declaration this build never
// opened is what its renderer cannot measure. The same tree with the declaration
// merely absent still answers a report whose dependency node reads as unassigned.
func TestDependencyLayerMapThisBuildCannotOpenRefusesTheModuleReport(t *testing.T) {
	root := t.TempDir()
	foreign := filepath.Join(root, "foundation", layerFile)
	if err := os.MkdirAll(foreign, 0o755); err != nil {
		t.Fatal(err)
	}
	p := packageFixture("modules/a")
	p.Dir, p.Module.Dir = filepath.Join(root, "modules/a"), root
	p.Imports = []string{dependency + "/modules/b/contracts"}
	base := fakeCommand(t, root, jsonStream(t, p))
	call := func(ctx context.Context, directory string, args ...string) ([]byte, error) {
		if strings.Join(args, " ") == "list -m -json "+dependency {
			return jsonStream(t, module{Path: dependency, Dir: filepath.Join(root, "foundation")}), nil
		}
		return base(ctx, directory, args...)
	}
	args := []string{"--modules", root, dependency}
	var reported, refused bytes.Buffer
	if status := run(t.Context(), args, &reported, &refused, call); status != 2 || reported.Len() != 0 ||
		!strings.Contains(refused.String(), foreign) || strings.Contains(refused.String(), "OUT OF BOUNDS") {
		t.Fatalf("unopenable declaration: status %d, stdout %s, stderr %s, want 2 refused naming %s with nothing reported",
			status, &reported, &refused, foreign)
	}
	if err := os.RemoveAll(foreign); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if status := run(t.Context(), args, &stdout, &stderr, call); status != 0 ||
		!strings.Contains(stderr.String(), "module-layer-missing") {
		t.Fatalf("absent declaration: status %d, stdout %s, stderr %s, want 0 with a report and the missing row named",
			status, &stdout, &stderr)
	}
	var report moduleReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("absent declaration reports no readable graph: %v; %s", err, &stdout)
	}
	foreignKey := dependency + "/modules/b"
	unassigned := false
	for _, node := range report.Nodes {
		if node.Module == foreignKey && node.Layer == layerUnassigned {
			unassigned = true
		}
	}
	if !unassigned {
		t.Errorf("the report names no unassigned node for %s; got %s", foreignKey, &stdout)
	}
}
