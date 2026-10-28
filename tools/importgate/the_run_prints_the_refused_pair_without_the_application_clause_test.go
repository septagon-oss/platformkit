package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestTheRunPrintsTheRefusedPairWithoutTheApplicationClause reads the application
// clause of a refusal through a whole run rather than one function: the checked
// repository declares one module, a composition package under its apps/ tree imports
// that module's test helper, and the answer is status 1 with one
// test-helper-in-production refusal whose line names the rule, the pair and the advice
// with nothing between the rule and the pair. The clause that says a reach came from an
// application belongs to the one rule that refuses a reach into a module's internals, so
// a refusal that came from a helper import prints the pair alone: the reach was real but
// the internals were not what it touched. Inputs: a fixture repository with one module,
// its contracts helper, one apps/ package that imports it and the layer row the module
// declares. Failure: an accepted build, or a printed refusal that blames an application
// for the wrong kind of reach, which is what a clause keyed on the refusing package
// alone answers while every reading of one refusal's line still matches its substring.
func TestTheRunPrintsTheRefusedPairWithoutTheApplicationClause(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-buildvcs=false")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
	root := t.TempDir()
	application := product + "/apps/example/composition"
	helper := product + "/modules/b/contracts/btest"
	writeFixture(t, root, "go.mod", "module "+product+"\n\ngo 1.26.6\n")
	writeFixture(t, root, "modules/b/contracts/btest/helper.go", "package btest\n")
	writeFixture(t, root, "apps/example/composition/composition.go",
		"package composition\n\nimport _ \""+helper+"\"\n")
	writeFixture(t, root, layerFile, product+"/modules/b generic\n")
	var stdout, stderr bytes.Buffer
	if code := run(t.Context(), []string{root}, &stdout, &stderr, runGo); code != 1 {
		t.Fatalf("status %d, want 1 refused for a helper an application imported: stdout %s stderr %s", code, &stdout, &stderr)
	}
	text := stderr.String()
	if !strings.Contains(text, "OUT OF BOUNDS [test-helper-in-production] "+application+" -> "+helper+
		" (production): "+remedies["test-helper-in-production"]+"\n") {
		t.Errorf("the refusal does not print its own rule, pair and advice: %s", text)
	}
	if strings.Contains(text, "apps/ reaches into: ") {
		t.Errorf("the application clause stands before a refusal that reached no internals: %s", text)
	}
	if strings.Contains(stdout.String(), "satisfy the module boundaries") {
		t.Errorf("a helper an application imported answered with an accepted build: %s", &stdout)
	}
}
