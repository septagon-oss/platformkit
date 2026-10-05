package seed

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// House rule 7: a field nothing reads is not written. A Snapshot is what a Writer
// hands the service about the owner's row, so every field of it is one the
// service's own code reads; a field a writer may fill and nothing consults is a
// promise the port does not keep.
func TestEverySnapshotFieldAWriterFillsIsReadByTheService(t *testing.T) {
	ports := map[string]bool{"Snapshot": true}
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
	}
	declared := make(map[string]string)
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			spec, ok := n.(*ast.TypeSpec)
			if !ok || !ports[spec.Name.Name] {
				return true
			}
			if st, ok := spec.Type.(*ast.StructType); ok {
				for _, field := range st.Fields.List {
					for _, name := range field.Names {
						declared[name.Name] = spec.Name.Name
					}
				}
			}
			return true
		})
	}
	read := make(map[string]bool)
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				read[sel.Sel.Name] = true
			}
			return true
		})
	}
	for field, typ := range declared {
		if !read[field] {
			t.Errorf("%s.%s is what a Writer reports and no code in kit/seed reads it", typ, field)
		}
	}
}
