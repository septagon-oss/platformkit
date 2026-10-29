package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheckedRepositorysOwnLayerMapThatCannotBeOpenedRefusesTheRun checks the same
// reading failure on the near side, where the declaration is this repository's: the
// file sits at the path the gate reads and holds no bytes this build can open — a
// directory where a checkout wrote a file. The run answers status 2 and names that
// file. Read as "declares nothing" instead, the same tree answers a policy verdict
// that every module of the checked repository is unmapped: a claim about a file this
// build never opened, sent to a reader who cannot write rows into the path it names.
// The report mode is asked too, because that is where an unread map becomes
// something a machine consumes: a policy verdict still prints the module graph with
// every node unassigned, so only a reading failure leaves stdout empty.
func TestCheckedRepositorysOwnLayerMapThatCannotBeOpenedRefusesTheRun(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-buildvcs=false")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
	root := t.TempDir()
	writeFixture(t, root, "go.mod", "module "+product+"\n\ngo 1.26.6\n")
	writeFixture(t, root, "modules/a/a.go", "package a\n")
	if err := os.MkdirAll(filepath.Join(root, layerFile), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{root}, {"--modules", root}} {
		var stdout, stderr bytes.Buffer
		if code := run(t.Context(), args, &stdout, &stderr, runGo); code != 2 ||
			stdout.Len() != 0 || !strings.Contains(stderr.String(), filepath.Join(root, layerFile)) ||
			strings.Contains(stderr.String(), "OUT OF BOUNDS") {
			t.Fatalf("%v: status %d, stdout %s, stderr %s, want 2 refused for the unopenable %s with no verdict",
				args, code, &stdout, &stderr, filepath.Join(root, layerFile))
		}
	}
}
