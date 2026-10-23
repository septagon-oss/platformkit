package resource

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoPluralIsBuiltByAppendingS pins that the resource screens take plurals
// from the catalogue and never append an "s" to a name.
func TestNoPluralIsBuiltByAppendingS(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no sources found: %v", err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, `+ "s"`) || strings.Contains(line, `"%ss"`) {
				t.Errorf("%s:%d builds a plural by appending s: %s", f, i+1, strings.TrimSpace(line))
			}
		}
	}
}
