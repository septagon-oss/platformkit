package rest_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoFunctionInThePackageRunsPastOneHundredLines keeps the resource
// operations readable as steps: the package's own non-test functions stay short.
func TestNoFunctionInThePackageRunsPastOneHundredLines(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			if _, ok := d.(*ast.FuncDecl); !ok {
				continue
			}
			start, end := fset.Position(d.Pos()).Line, fset.Position(d.End()).Line
			if end-start+1 > 100 {
				t.Errorf("%s:%d declaration spans %d lines", name, start, end-start+1)
			}
		}
	}
}
