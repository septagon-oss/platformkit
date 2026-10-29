package exampletest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestEveryListedPackageExecutesAtLeastOneExampleWithOutput(t *testing.T) {
	for _, pkg := range []string{"db", "events", "httpx", "rest", "crud", "cache", "jobs", "tenancy"} {
		file, err := parser.ParseFile(token.NewFileSet(), "../../"+pkg+"/example_test.go", nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("%s: %v", pkg, err)
		}
		executed := 0
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !strings.HasPrefix(fn.Name.Name, "Example") || fn.Recv != nil {
				continue
			}
			for _, group := range file.Comments {
				if group.Pos() > fn.Body.Rbrace && group.End() < fn.End()+1 || group.Pos() > fn.Body.Lbrace && group.End() < fn.Body.Rbrace {
					text := group.Text()
					if strings.HasPrefix(text, "Output:") || strings.HasPrefix(text, "Unordered output:") {
						executed++
					}
				}
			}
		}
		if executed == 0 {
			t.Errorf("%s/example_test.go has no example with an Output directive; go test would only compile it", pkg)
		}
	}
}
