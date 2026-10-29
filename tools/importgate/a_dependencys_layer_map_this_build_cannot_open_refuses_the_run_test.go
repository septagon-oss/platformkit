package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDependencyLayerMapThisBuildCannotOpenRefusesTheRun checks the shape of a
// declared dependency's declaration no row can express: the path is there and this
// build cannot open it — a directory where a checkout wrote a file, a mount, a
// permission no reader holds. Reading it is what the whole declaration is worth,
// so the run answers as it answers every other reading failure: status 2, the
// diagnostic naming that dependency's file, nothing on stdout. Treated as a missing
// file instead, the same tree answers a policy verdict naming the dependency's
// modules as unmapped, which is a claim about a file this build never read.
func TestDependencyLayerMapThisBuildCannotOpenRefusesTheRun(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "modules/a")
	foreign := filepath.Join(root, "foundation", layerFile)
	if err := os.MkdirAll(foreign, 0o755); err != nil {
		t.Fatal(err)
	}
	p := packageFixture("modules/a")
	p.Dir, p.Module.Dir = source, root
	p.Imports = []string{dependency + "/modules/b/contracts"}
	base := fakeCommand(t, root, jsonStream(t, p))
	call := func(ctx context.Context, directory string, args ...string) ([]byte, error) {
		if strings.Join(args, " ") == "list -m -json "+dependency {
			return jsonStream(t, module{Path: dependency, Dir: filepath.Join(root, "foundation")}), nil
		}
		return base(ctx, directory, args...)
	}
	var stdout, stderr bytes.Buffer
	if status := run(t.Context(), []string{root, dependency}, &stdout, &stderr, call); status != 2 ||
		stdout.Len() != 0 || !strings.Contains(stderr.String(), foreign) ||
		strings.Contains(stderr.String(), "OUT OF BOUNDS") {
		t.Fatalf("status %d, stdout %s, stderr %s, want 2 refused naming %s with no verdict", status, &stdout, &stderr, foreign)
	}
}
