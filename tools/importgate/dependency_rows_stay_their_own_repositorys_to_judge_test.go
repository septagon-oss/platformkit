package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDependencyLayerDeclarationThisBuildCannotReadRefusesTheRun checks the two
// shapes in which a declared dependency's map is unreachable: a root Go resolved
// with no directory at all, and a vendored build, where a dependency's tree holds
// only the packages the main module imports. Both are reading failures, so the run
// answers status 2 naming the dependency. It must not answer status 1, because a
// policy verdict over a map that was never read would tell the reader every module
// of that repository is unmapped, which the build does not know.
func TestDependencyLayerDeclarationThisBuildCannotReadRefusesTheRun(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "modules/a")
	if err := os.MkdirAll(filepath.Join(root, "foundation", "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, root, "foundation/scripts/imports-layers.txt", dependency+"/modules/b platform\n")
	p := packageFixture("modules/a")
	p.Dir, p.Module.Dir = source, root
	p.Imports = []string{dependency + "/modules/b/contracts"}
	records := jsonStream(t, p)
	for _, shape := range []struct{ name, resolved, want string }{
		{"root reports no directory", "", "reports no directory"},
		{"vendored build", filepath.Join(root, "foundation"), "vendored build"},
	} {
		t.Run(shape.name, func(t *testing.T) {
			if shape.name == "vendored build" {
				writeFixture(t, root, "vendor/modules.txt", "# "+dependency+"\n"+dependency+"/modules/b/contracts\n")
			}
			base := fakeCommand(t, root, records)
			call := func(ctx context.Context, directory string, args ...string) ([]byte, error) {
				if strings.Join(args, " ") == "list -m -json "+dependency {
					return jsonStream(t, module{Path: dependency, Dir: shape.resolved}), nil
				}
				return base(ctx, directory, args...)
			}
			var stdout, stderr bytes.Buffer
			if status := run(t.Context(), []string{root, dependency}, &stdout, &stderr, call); status != 2 ||
				stdout.Len() != 0 || !strings.Contains(stderr.String(), shape.want) ||
				!strings.Contains(stderr.String(), dependency) {
				t.Fatalf("status %d, stdout %s, stderr %s", status, &stdout, &stderr)
			}
		})
	}
}

// TestJustificationRowOfADependencyNamesTheGateThatJudgesIt checks the notice a
// consumer prints for a pair row it cannot measure. Saying only "not reciprocal in
// production" over a pair of another repository's modules would invite a reader to
// delete a row that is true where it is written, so the line names the repository
// whose gate measures that pair, as the module-layer-missing remedy already does.
func TestJustificationRowOfADependencyNamesTheGateThatJudgesIt(t *testing.T) {
	g := fixtureGraph()
	pair := dependency + "/modules/b <-> " + dependency + "/modules/c Both contracts name each other's values.\n"
	foreign := newLayerDeclaration()
	if err := parseLayers("foundation/scripts/imports-layers.txt", pair, g, dependency, foreign); err != nil {
		t.Fatal(err)
	}
	notice := foreign.notices(nil, nil)
	if !strings.Contains(notice, dependency+" judges this row") || strings.Contains(notice, "not reciprocal") {
		t.Fatalf("a dependency's row is announced as this build's own verdict: %s", notice)
	}
	own := newLayerDeclaration()
	ownRow := strings.ReplaceAll(pair, dependency+"/modules/", product+"/modules/")
	if err := parseLayers("scripts/imports-layers.txt", ownRow, g, product, own); err != nil {
		t.Fatal(err)
	}
	if notice = own.notices(nil, nil); !strings.Contains(notice, "not reciprocal in production in this build context") ||
		strings.Contains(notice, product+" judges this row") {
		t.Fatalf("a row of the checked repository is announced as another repository's: %s", notice)
	}
}
