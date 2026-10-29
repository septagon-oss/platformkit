package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestAJustificationRowNeverAdmitsAnUpwardImport checks that a pair's row is a
// statement about reciprocity and never a waiver: over two modules of different
// layers whose production source reaches each other both ways, the row is spent —
// the run announces the pair as reciprocal in this build context — and the
// direction that climbs the order is still refused. Every other pair row on this
// branch's tree joins modules of one layer, so nothing else asks whether an
// exception can be read across a layer. Inputs: a fixture repository whose
// platform module imports down into a product module's contracts and back, with
// the pair's row in scripts/imports-layers.txt. Failure: status 0, which would
// mean the row admitted the upward import.
func TestAJustificationRowNeverAdmitsAnUpwardImport(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-buildvcs=false")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
	const reason = "Both contract trees name each other's values, and neither direction moves without reshaping a published surface."
	root := t.TempDir()
	writeFixture(t, root, "go.mod", "module "+product+"\n\ngo 1.26.6\n")
	below, above := product+"/modules/lower", product+"/modules/upper"
	writeFixture(t, root, "modules/lower/contracts/values.go", "package contracts\nimport _ \""+above+"/contracts\"\n")
	writeFixture(t, root, "modules/upper/contracts/values.go", "package contracts\n")
	writeFixture(t, root, "modules/upper/upper.go", "package upper\nimport _ \""+below+"/contracts\"\n")
	writeFixture(t, root, layerFile, below+" platform\n"+above+" product\n"+below+" <-> "+above+" "+reason+"\n")
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), []string{root}, &stdout, &stderr, runGo)
	if code == 0 {
		t.Fatalf("a pair's row admitted an upward production import: status %d, stdout %s, stderr %s", code, &stdout, &stderr)
	}
	if refusals := strings.Count(stderr.String(), "OUT OF BOUNDS"); refusals != 1 {
		t.Fatalf("refusals = %d, want the one upward import of this tree: stdout %s stderr %s", refusals, &stdout, &stderr)
	}
	want := "OUT OF BOUNDS [module-layer-upward] " + below + "/contracts -> " + above + "/contracts"
	if !strings.Contains(stderr.String(), want) {
		t.Errorf("the run refused something else: want %q in stderr %s", want, &stderr)
	}
	if !strings.Contains(stderr.String(), below+" <-> "+above+"; reciprocal in production in this build context") {
		t.Errorf("the row was not announced as spent by this build: stderr %s", &stderr)
	}
}
