package resource_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestResourceRetiresLocalPresentationSelection(t *testing.T) {
	retired := map[string]bool{"reading": true, "primary": true, "rowName": true, "displayField": true, "displayName": true, "listFields": true, "known": true, "candidates": true, "onList": true, "readable": true, "label": true, "sortable": true, "next": true, "direction": true}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if f, ok := n.(*ast.FuncDecl); ok && retired[f.Name.Name] {
				t.Errorf("retired selector %s in %s", f.Name.Name, name)
			}
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "display" && sel.Sel.Name == "Plural" {
					t.Errorf("local plural selection in %s", name)
				}
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "r" && sel.Sel.Name == "Present" && name != "policy.go" {
					t.Errorf("raw resource hints in %s", name)
				}
			}
			return true
		})
	}
}
