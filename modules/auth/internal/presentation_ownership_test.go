package internal_test

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestAuthPageMarkupLivesInInternalUI(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("read imports of %s: %v", name, err)
		}
		for _, imported := range file.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatalf("read import of %s: %v", name, err)
			}
			if path == "maragu.dev/gomponents" || strings.HasPrefix(path, "maragu.dev/gomponents/") ||
				path == "github.com/septagon-oss/platformkit/ui/components" ||
				path == "github.com/septagon-oss/platformkit/ui/document" {
				t.Errorf("%s imports presentation package %q outside internal/ui", name, path)
			}
		}
	}
}
