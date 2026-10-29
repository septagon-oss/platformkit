package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// TestHalfReadLayerMapRefusesTheRunAtItsLine checks what a person gets when their
// declaration says one thing twice: the whole run stops as an incomplete reading
// (status 2), stdout carries no acceptance, and the diagnostic names the file and
// the physical line of the repeated row. Comment and blank lines count towards that
// number, so it points at the row to delete rather than at its place among the data
// rows — the header of a committed map runs to two dozen lines.
func TestHalfReadLayerMapRefusesTheRunAtItsLine(t *testing.T) {
	for _, shape := range []struct{ repeat, want string }{
		{product + "/modules/a platform", "duplicate module " + product + "/modules/a"},
		{product + "/modules/b <-> " + product + "/modules/a The same pair, the other way round.",
			"duplicate pair " + product + "/modules/a <-> " + product + "/modules/b"},
	} {
		t.Setenv("GOWORK", "off")
		t.Setenv("GOFLAGS", "-buildvcs=false")
		t.Setenv("GOPROXY", "off")
		t.Setenv("GOSUMDB", "off")
		root := t.TempDir()
		writeFixture(t, root, "go.mod", "module "+product+"\n\ngo 1.26.6\n")
		writeFixture(t, root, "modules/a/a.go", "package a\n")
		writeFixture(t, root, "modules/b/b.go", "package b\n")
		rows := []string{"# Every module of this repository, in one layer.", "",
			product + "/modules/a platform", product + "/modules/b platform",
			product + "/modules/a <-> " + product + "/modules/b Both directions stay.", shape.repeat}
		writeFixture(t, root, "scripts/imports-layers.txt", strings.Join(rows, "\n")+"\n")
		var stdout, stderr bytes.Buffer
		code := run(t.Context(), []string{root}, &stdout, &stderr, runGo)
		want := fmt.Sprintf("%s:%d: %s", filepath.Join(root, "scripts", "imports-layers.txt"), len(rows), shape.want)
		if code != 2 || !strings.Contains(stderr.String(), want) {
			t.Errorf("status %d, want 2 refused for %q: stderr %s", code, want, &stderr)
		}
		if strings.Contains(stdout.String(), "satisfy the module boundaries") {
			t.Errorf("a map that could not be read answered with a verdict: %s", &stdout)
		}
	}
}
