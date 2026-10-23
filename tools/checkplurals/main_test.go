package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlantedPluralSuffixIsRefused(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"messages", filepath.Join("kit", "entity", "display"), filepath.Join("kit", "entity", "other")} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	const suffix = "package planted\nvar x = noun + \"s\"\n"
	for path, body := range map[string]string{
		"bad.go":                        "package planted\nvar x = noun + \"s\"\nvar y = \"%ss\"\n",
		"messages/en.json":              `{"key":{"translation":"No %ss"}}`,
		"good_test.go":                  "package planted\nvar x = noun + \"s\"\n",
		"kit/entity/display/display.go": suffix,
		"kit/entity/display/other.go":   suffix,
		"kit/entity/other/display.go":   suffix,
	} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	problems, err := scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 5 {
		t.Fatalf("wanted Go suffix, Go format, catalogue format and the two sibling files refused; got %v", problems)
	}
	for _, problem := range problems {
		if strings.Contains(problem, "good_test.go") {
			t.Fatalf("a test fixture was scanned: %s", problem)
		}
		if strings.Contains(problem, filepath.ToSlash(filepath.Join("kit/entity/display", "display.go"))) {
			t.Fatalf("the frozen English helper was refused: %s", problem)
		}
	}
}
