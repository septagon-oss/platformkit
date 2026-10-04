package tenanttest_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestTheTenantConformanceSuiteDecidesTheOwnLanguageCommand(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "conformance.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	ast.Inspect(file, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "SetOwnLocale" {
			called = true
		}
		return true
	})
	if !called {
		t.Error("the shared tenant conformance suite never exercises SetOwnLocale against the fake and SQL service")
	}
}
