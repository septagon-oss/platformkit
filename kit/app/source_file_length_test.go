package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAppSourceFilesStayReadable pins the split of this package: no production
// file here grows back past the 800-line ceiling the kernel's complexity gate uses.
func TestAppSourceFilesStayReadable(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(string(b), "\n"); n > 800 {
			t.Errorf("%s has %d lines, over 800", f, n)
		}
	}
}
