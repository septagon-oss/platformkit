package db_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// The refusal of a two-statement data body belongs beside the other data-file
// shape cases, so a reader looking for that behavior finds its existing owner.
func TestDataFileShapeRefusalLivesWithShapeTests(t *testing.T) {
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
			if !ok || function.Name.Name != "TestARefusedDataFileShapeLeavesNoDrainBehind" {
				continue
			}
			found = true
			if name != "data_file_shape_test.go" {
				t.Errorf("the two-statement refusal is in %s; fold it into data_file_shape_test.go, which already owns data-file shape cases", name)
			}
		}
	}
	if !found {
		t.Fatal("the two-statement refusal test is missing")
	}
}
