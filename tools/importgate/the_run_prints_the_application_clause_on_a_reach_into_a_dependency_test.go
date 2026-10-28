package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// TestTheRunPrintsTheApplicationClauseOnAReachIntoADependency reads the application
// clause of a refusal out of a build that compiles: the checked repository's own module
// sits inside the dependency module's tree, which is the one layout where Go's internal
// package rule leaves a graph for the gate to read while the import still crosses that
// boundary. The answer is status 1 with one foreign-internal refusal whose line carries
// the clause between the rule and the refusing package, the pair, the context and the
// advice. The clause is what tells a person the reach came from an application rather
// than from a peer module, so a refusal that prints the pair alone reads as a reach
// nobody warned about. Inputs: the dependency with one module, its contracts package and
// an internal package; the checked repository with its own module row, a contracts
// package and an apps/ composition package that imports the internals. Failure: an
// accepted build, or a foreign-internal refusal that prints no clause.
func TestTheRunPrintsTheApplicationClauseOnAReachIntoADependency(t *testing.T) {
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
	writeFixture(t, root, "apps/example/composition/composition.go",
		"package composition\n\nimport _ \""+dependency+"/modules/b/internal/x\"\n")
	writeFixture(t, root, layerFile, nested+"/modules/a product\n")
	var stdout, stderr bytes.Buffer
	if code := run(t.Context(), []string{root, dependency}, &stdout, &stderr, runGo); code != 1 {
		t.Fatalf("status %d, want 1 refused for a reach into a dependency's internals: stdout %s stderr %s", code, &stdout, &stderr)
	}
	want := "OUT OF BOUNDS [foreign-internal] apps/ reaches into: " + nested + "/apps/example/composition -> " +
		dependency + "/modules/b/internal/x (production): " + remedies["foreign-internal"] + "\n"
	if !strings.Contains(stderr.String(), want) {
		t.Errorf("the reach into a dependency's internals printed no application clause: %s", &stderr)
	}
}
