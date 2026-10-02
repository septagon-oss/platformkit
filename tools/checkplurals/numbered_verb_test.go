package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestANumberedPluralFormatIsRefused: gotext catalogues number their verbs, so
// "%[1]ss" is the same suffix as "%ss" and is refused in Go and in catalogues.
func TestANumberedPluralFormatIsRefused(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "messages"), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		"bad.go":              "package planted\nvar y = \"%[1]ss\"\n",
		"messages/pt-PT.json": `{"key":{"translation":"Ainda sem %[1]ss."}}`,
	} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	problems, err := scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 2 {
		t.Fatalf("wanted the Go and the catalogue numbered-verb refusals; got %v", problems)
	}
}
