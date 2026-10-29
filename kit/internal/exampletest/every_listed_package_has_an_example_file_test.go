package exampletest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEveryListedPackageDeclaresAnExampleFunction(t *testing.T) {
	for _, pkg := range []string{"db", "events", "httpx", "rest", "crud", "cache", "jobs", "tenancy"} {
		body, err := os.ReadFile(filepath.Join("..", "..", pkg, "example_test.go"))
		if err != nil {
			t.Errorf("kit/%s: %v", pkg, err)
			continue
		}
		if !strings.Contains(string(body), "func Example") {
			t.Errorf("kit/%s/example_test.go declares no Example function", pkg)
		}
	}
}
