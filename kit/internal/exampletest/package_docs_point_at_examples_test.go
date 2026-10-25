package exampletest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEveryListedPackageDocNamesItsExamples(t *testing.T) {
	for _, pkg := range []string{"db", "events", "httpx", "rest", "crud", "cache", "jobs", "tenancy"} {
		matches, err := filepath.Glob(filepath.Join("..", "..", pkg, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, file := range matches {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			body, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(strings.ToLower(string(body)), "example") {
				found = true
			}
		}
		if !found {
			t.Errorf("kit/%s: no non-test file points at its examples", pkg)
		}
	}
}
