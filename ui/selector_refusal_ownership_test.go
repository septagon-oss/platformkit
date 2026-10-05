package ui_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// At-rule selector refusals belong with the table that also accepts at-signs as
// data. Keeping the smaller table separately duplicates the same three inputs.
func TestAtRuleSelectorRefusalsHaveOneTableOwner(t *testing.T) {
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, name := range files {
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok {
				continue
			}
			switch function.Name.Name {
			case "TestTheGateRefusesASelectorThatIsAnAtRulePreludeAndReadsAnAtSignAsData":
				found = true
				if name != "ui_test.go" {
					t.Errorf("selector table is in %s, want ui_test.go", name)
				}
			case "TestTheGateRefusesASelectorThatIsAnAtRulePrelude":
				t.Errorf("%s retains the smaller at-rule table; fold its inputs into ui_test.go's selector table", name)
			}
		}
	}
	if !found {
		t.Fatal("the selector table with refusal and acceptance cases is missing")
	}
}
