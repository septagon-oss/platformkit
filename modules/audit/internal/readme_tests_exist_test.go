package internal_test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestEveryTestTheREADMENamesExists: the module's README cites tests as the evidence
// for its duties, and a citation of a test nobody can run is a claim with nothing
// behind it.
func TestEveryTestTheREADMENamesExists(t *testing.T) {
	readme, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{}
	decl := regexp.MustCompile(`(?m)^func (Test\w+)\(`)
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range decl.FindAllSubmatch(body, -1) {
			declared[string(m[1])] = true
		}
	}
	cited := regexp.MustCompile("`(Test\\w+)`").FindAllSubmatch(readme, -1)
	if len(cited) == 0 {
		t.Fatal("the README cites no test, which proves nothing")
	}
	for _, m := range cited {
		if name := string(m[1]); !declared[name] {
			t.Errorf("README.md cites %s, which no test in modules/audit/internal declares", name)
		}
	}
}
