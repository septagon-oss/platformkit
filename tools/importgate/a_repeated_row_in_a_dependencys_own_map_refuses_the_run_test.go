package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// TestRepeatedRowInADependencysOwnMapRefusesTheRun checks the run a consumer gets
// when a declared dependency's own declaration says one thing twice while the
// consumer's map holds nothing wrong: status 2 as an incomplete reading, the
// diagnostic naming that dependency's file and the physical line of the row that
// repeats, and no acceptance on stdout. The refusal has to name the repository
// whose file it is, because a consumer cannot repair it, and a repeated row read
// as a verdict would let a map that disagrees with itself through: the later row
// would simply win the module's layer.
func TestRepeatedRowInADependencysOwnMapRefusesTheRun(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-buildvcs=false")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
	root, dependencyDir := t.TempDir(), t.TempDir()
	writeFixture(t, dependencyDir, "go.mod", "module "+dependency+"\n\ngo 1.26.6\n")
	writeFixture(t, dependencyDir, "modules/b/contracts/values.go", "package contracts\n")
	// A comment and a blank precede the rows, so the repeated row is the fourth line
	// of the file and the second data row: the number a reader opens is the first.
	rows := []string{"# Every module of this repository, in one layer.", "",
		dependency + "/modules/b platform", dependency + "/modules/b platform"}
	writeFixture(t, dependencyDir, layerFile, strings.Join(rows, "\n")+"\n")
	writeFixture(t, root, "go.mod", fmt.Sprintf("module %s\n\ngo 1.26.6\nrequire %s v0.0.0\nreplace %s => %q\n",
		product, dependency, dependency, dependencyDir))
	writeFixture(t, root, "modules/a/a.go", "package a\nimport _ \""+dependency+"/modules/b/contracts\"\n")
	writeFixture(t, root, layerFile, product+"/modules/a platform\n")
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), []string{root, dependency}, &stdout, &stderr, runGo)
	want := fmt.Sprintf("%s:%d: duplicate module %s", filepath.Join(dependencyDir, layerFile),
		len(rows), dependency+"/modules/b")
	if code != 2 || !strings.Contains(stderr.String(), want) {
		t.Errorf("status %d, want 2 refused for %q: stderr %s", code, want, &stderr)
	}
	if strings.Contains(stdout.String(), "satisfy the module boundaries") {
		t.Errorf("a dependency's map whose rows disagree answered with a passing build: %s", &stdout)
	}
}
