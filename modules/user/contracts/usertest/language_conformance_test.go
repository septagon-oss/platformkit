package usertest_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// TestTheConformanceSuiteDecidesTheLanguageCommand: the fake and the SQL service
// make the same decision only if the shared suite RunService runs both through
// SetPreferredLocale (decision 0010: every port has a fake and a conformance suite).
func TestTheConformanceSuiteDecidesTheLanguageCommand(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "conformance.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	ast.Inspect(file, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "SetPreferredLocale" {
			called = true
		}
		return true
	})
	if !called {
		t.Errorf("usertest's conformance suite never calls SetPreferredLocale: the fake's language decisions are checked against nothing")
	}
}
