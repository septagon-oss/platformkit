package exampletest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryDeclaredExampleIsExecutedByName requires each example a documented
// package declares to carry an Output directive and to be named in that
// package's call to Execute. An example without a directive is compiled by
// go test and asserts nothing, and a name left out of Execute proves only that
// the other examples ran. The per-package checks below pass while an added
// example runs neither way, so this one names each declared example.
func TestEveryDeclaredExampleIsExecutedByName(t *testing.T) {
	for _, pkg := range []string{"db", "events", "httpx", "rest", "crud", "cache", "jobs", "tenancy"} {
		dir := filepath.Join("..", "..", pkg)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("kit/%s: %v", pkg, err)
		}
		fset := token.NewFileSet()
		declared, directed, executed := map[string]bool{}, map[string]bool{}, map[string]bool{}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "Example") {
					continue
				}
				declared[fn.Name.Name] = true
				for _, group := range file.Comments {
					text := group.Text()
					inside := group.Pos() > fn.Body.Lbrace && group.End() < fn.Body.Rbrace
					if inside && (strings.HasPrefix(text, "Output:") || strings.HasPrefix(text, "Unordered output:")) {
						directed[fn.Name.Name] = true
					}
				}
			}
			for _, name := range executedNames(file) {
				executed[name] = true
			}
		}
		if len(declared) == 0 {
			t.Errorf("kit/%s declares no Example function", pkg)
		}
		for name := range declared {
			if !directed[name] {
				t.Errorf("kit/%s %s has no Output directive: go test compiles it and checks nothing", pkg, name)
			}
			if !executed[name] {
				t.Errorf("kit/%s %s is not named in the package's exampletest.Execute call", pkg, name)
			}
		}
	}
}

// executedNames returns the example names one package test asks Execute to
// rerun, which are the string arguments after the testing.T parameter.
func executedNames(file *ast.File) []string {
	var names []string
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Execute" {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "exampletest" || len(call.Args) < 2 {
			return true
		}
		for _, arg := range call.Args[1:] {
			if lit, ok := arg.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				names = append(names, strings.Trim(lit.Value, `"`))
			}
		}
		return true
	})
	return names
}
