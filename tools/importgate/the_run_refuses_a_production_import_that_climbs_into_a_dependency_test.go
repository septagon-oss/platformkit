package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// TestTheRunRefusesAProductionImportThatClimbsIntoADependency checks the platform's
// module order through a whole run rather than one function: the checked repository
// declares its own module generic, a declared dependency ranks its own module
// product, a production package of the first imports a contracts package of the
// second, and the answer is status 1 with one module-layer-upward refusal naming both
// repositories' packages and the layer each declared for its own module. The report,
// the verdict and the process status all read the one declaration the run assembled,
// so a build that documented the far side's layer and then judged the import against
// this repository's rows alone would answer with an accepted build and status 0 while
// its own report printed the upward edge.
func TestTheRunRefusesAProductionImportThatClimbsIntoADependency(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-buildvcs=false")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
	root, dependencyDir := t.TempDir(), t.TempDir()
	writeFixture(t, dependencyDir, "go.mod", "module "+dependency+"\n\ngo 1.26.6\n")
	writeFixture(t, dependencyDir, "modules/b/contracts/values.go", "package contracts\n")
	writeFixture(t, dependencyDir, layerFile, dependency+"/modules/b product\n")
	writeFixture(t, root, "go.mod", fmt.Sprintf("module %s\n\ngo 1.26.6\nrequire %s v0.0.0\nreplace %s => %q\n",
		product, dependency, dependency, dependencyDir))
	writeFixture(t, root, "modules/a/a.go", "package a\nimport _ \""+dependency+"/modules/b/contracts\"\n")
	writeFixture(t, root, layerFile, product+"/modules/a generic\n")
	var stdout, stderr bytes.Buffer
	if code := run(t.Context(), []string{root, dependency}, &stdout, &stderr, runGo); code != 1 {
		t.Fatalf("status %d, want 1 refused for one upward import: stdout %s stderr %s", code, &stdout, &stderr)
	}
	text := stderr.String()
	for _, want := range []string{
		"OUT OF BOUNDS [module-layer-upward]",
		product + "/modules/a -> " + dependency + "/modules/b/contracts",
		"row reason: " + product + "/modules/a generic -> " + dependency + "/modules/b product",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the refusal does not carry %q: %s", want, text)
		}
	}
	if strings.Contains(stdout.String(), "satisfy the module boundaries") {
		t.Errorf("an import that climbs out of this repository answered with an accepted build: %s", &stdout)
	}
}
