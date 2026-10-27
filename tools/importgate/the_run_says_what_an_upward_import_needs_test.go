package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestTheRunSaysWhatAnUpwardImportNeeds checks the advice the run prints beside the
// refusal of an upward production import: the sentence after the pair is the answer
// a reader acts on, and it must name the port the lower module declares and say that
// no pair's row admits the import. The refusal itself is computed from the two
// declared layers alone, so a row cannot reach it; a map that carries this rule only
// in that sentence can lose the sentence while every measured case stays green.
// Inputs: a fixture repository whose platform module imports down into a product
// module's contracts, one direction only, so the upward refusal is all this tree
// refuses. Failure: the line carries no advice, or an advice that names no way out.
func TestTheRunSaysWhatAnUpwardImportNeeds(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-buildvcs=false")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
	root := t.TempDir()
	writeFixture(t, root, "go.mod", "module "+product+"\n\ngo 1.26.6\n")
	below, above := product+"/modules/lower", product+"/modules/upper"
	writeFixture(t, root, "modules/lower/contracts/values.go", "package contracts\nimport _ \""+above+"/contracts\"\n")
	writeFixture(t, root, "modules/upper/contracts/values.go", "package contracts\n")
	writeFixture(t, root, layerFile, below+" platform\n"+above+" product\n")
	var stdout, stderr bytes.Buffer
	if code := run(t.Context(), []string{root}, &stdout, &stderr, runGo); code == 0 {
		t.Fatalf("an upward production import passed: status %d, stdout %s, stderr %s", code, &stdout, &stderr)
	}
	const marker = "OUT OF BOUNDS [module-layer-upward] "
	lines := []string{}
	for _, line := range strings.Split(stderr.String(), "\n") {
		if strings.HasPrefix(line, "OUT OF BOUNDS [") {
			lines = append(lines, line)
		}
	}
	if len(lines) != 1 || !strings.HasPrefix(lines[0], marker) {
		t.Fatalf("this tree must refuse the upward import and nothing else, refused %q: stdout %s stderr %s", lines, &stdout, &stderr)
	}
	_, advice, found := strings.Cut(strings.TrimPrefix(lines[0], marker), "): ")
	if !found {
		t.Fatalf("the refusal carries no advice: %q", lines[0])
	}
	for _, want := range []string{"port the lower module declares", "no row admits an upward production import"} {
		if !strings.Contains(advice, want) {
			t.Errorf("the advice %q does not say %q, so the reader of this refusal has no way out to act on", advice, want)
		}
	}
}
