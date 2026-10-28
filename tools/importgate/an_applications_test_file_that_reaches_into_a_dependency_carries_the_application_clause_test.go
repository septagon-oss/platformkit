package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// TestAnApplicationsTestFileThatReachesIntoADependencyCarriesTheApplicationClause reads the
// application clause of a refusal whose import came from an external test package. A
// test file of an application package that reaches a dependency's internals is refused
// by the same rule as production source, and the clause names who reached, so the line
// carries the clause beside the test variant's package path. The checked repository sits
// inside the dependency module's tree, which is the layout where Go's internal package
// rule leaves a graph for the gate to read. Inputs: the dependency with one module, its
// contracts package and an internal package; the checked repository with a layer row, an
// apps/ package that imports nothing and an external test file of that package which
// imports the internals. Failure: a test-context refusal that prints its bare pair,
// which leaves a person reading a test failure without knowing the reach came from an
// application, or a clause printed for a rule that refuses something else.
func TestAnApplicationsTestFileThatReachesIntoADependencyCarriesTheApplicationClause(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-buildvcs=false")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
	dependencyDir, root := t.TempDir(), t.TempDir()
	nested := dependency + "/modules/b/apps/app"
	writeFixture(t, dependencyDir, "go.mod", "module "+dependency+"\n\ngo 1.26.6\n")
	writeFixture(t, dependencyDir, "modules/b/contracts/values.go", "package contracts\n")
	writeFixture(t, dependencyDir, "modules/b/internal/x/x.go", "package x\n")
	writeFixture(t, dependencyDir, layerFile, dependency+"/modules/b generic\n")
	writeFixture(t, root, "go.mod", fmt.Sprintf("module %s\n\ngo 1.26.6\nrequire %s v0.0.0\nreplace %s => %q\n",
		nested, dependency, dependency, dependencyDir))
	writeFixture(t, root, "modules/a/contracts/values.go", "package contracts\n")
	writeFixture(t, root, "apps/example/composition/composition.go", "package composition\n")
	writeFixture(t, root, "apps/example/composition/composition_test.go",
		"package composition_test\n\nimport _ \""+dependency+"/modules/b/internal/x\"\n")
	writeFixture(t, root, layerFile, nested+"/modules/a product\n")
	var stdout, stderr bytes.Buffer
	if code := run(t.Context(), []string{root, dependency}, &stdout, &stderr, runGo); code != 1 {
		t.Fatalf("status %d, want 1 refused for a test file's reach into a dependency's internals: stdout %s stderr %s",
			code, &stdout, &stderr)
	}
	if want := "OUT OF BOUNDS [foreign-internal] apps/ reaches into: " + nested + "/apps/example/composition_test -> " +
		dependency + "/modules/b/internal/x (external-test): " + remedies["foreign-internal"] + "\n"; !strings.Contains(stderr.String(), want) {
		t.Errorf("the test file's reach into a dependency's internals printed no application clause: %s", &stderr)
	}
}
