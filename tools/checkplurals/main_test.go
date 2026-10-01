package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlantedPluralSuffixIsRefused(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "messages"), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		"bad.go":           "package planted\nvar x = noun + \"s\"\nvar y = \"%ss\"\n",
		"messages/en.json": `{"key":{"translation":"No %ss"}}`,
		"good_test.go":     "package planted\nvar x = noun + \"s\"\n",
	} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	problems, err := scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 3 {
		t.Fatalf("wanted Go suffix, Go format, and catalogue format refusals; got %v", problems)
	}
	for _, problem := range problems {
		if strings.Contains(problem, "good_test.go") {
			t.Fatalf("a test fixture was scanned: %s", problem)
		}
	}
}
