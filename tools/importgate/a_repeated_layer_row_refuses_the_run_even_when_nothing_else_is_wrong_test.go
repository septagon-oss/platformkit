package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// TestRepeatedLayerRowRefusesTheRunEvenWhenNothingElseIsWrong checks the run a
// declaration gets whose only fault is one row repeating another: status 2 as an
// incomplete reading, the diagnostic naming the physical line of the repeated
// row, and no acceptance on stdout. Nothing else in this map can refuse — the two
// modules import nothing, so no pair row and no edge is at stake — which is what
// makes the missing acceptance mean something: a repeated row read as a verdict
// would let this map through as a passing build.
func TestRepeatedLayerRowRefusesTheRunEvenWhenNothingElseIsWrong(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-buildvcs=false")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
	root := t.TempDir()
	writeFixture(t, root, "go.mod", "module "+product+"\n\ngo 1.26.6\n")
	writeFixture(t, root, "modules/a/a.go", "package a\n")
	writeFixture(t, root, "modules/b/b.go", "package b\n")
	// A comment and a blank precede the rows, so the repeated row is the fifth line
	// of the file and the third data row: the number a reader opens is the first.
	rows := []string{"# Every module of this repository, in one layer.", "",
		product + "/modules/a platform", product + "/modules/b platform",
		product + "/modules/a platform"}
	writeFixture(t, root, layerFile, strings.Join(rows, "\n")+"\n")
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), []string{root}, &stdout, &stderr, runGo)
	want := fmt.Sprintf("%s:%d: duplicate module %s", filepath.Join(root, layerFile),
		len(rows), product+"/modules/a")
	if code != 2 || !strings.Contains(stderr.String(), want) {
		t.Errorf("status %d, want 2 refused for %q: stderr %s", code, want, &stderr)
	}
	if strings.Contains(stdout.String(), "satisfy the module boundaries") {
		t.Errorf("a map whose rows disagree answered with a passing build: %s", &stdout)
	}
}
