package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// splitSuites are the port-conformance suites whose named cases live in files
// beside the entry point that hands them a store.
var splitSuites = []string{
	filepath.Join("..", "cache", "cachetest"),
	filepath.Join("..", "inference", "inferencetest"),
	filepath.Join("..", "secrets", "secretstest"),
	filepath.Join("..", "push", "pushtest"),
}

// TestSplitConformanceCasesAreStillRun pins that every case of a split
// conformance suite is still named by something a caller can start.
//
// A suite is a table of named cases, and Go compiles a case nobody calls: one
// dropped `t.Run` line shortens the suite silently and the shorter suite still
// passes, so an adapter is checked for less than it was. The evidence that a case
// runs is a call. Each suite is walked as one package, and every function it
// declares has to be reachable from a name outside it can utter — an exported
// function, a test of its own, or a package-level table. A directory that
// declares nothing a caller can start is refused rather than passed, so the check
// cannot go green by looking at the wrong place.
func TestSplitConformanceCasesAreStillRun(t *testing.T) {
	for _, dir := range splitSuites {
		calls, starts := suiteCalls(t, dir)
		if len(starts) == 0 {
			t.Fatalf("%s declares nothing a caller can start: the suite moved and this check names the wrong directory", dir)
		}
		reached := map[string]bool{"": true}
		var walk func(string)
		walk = func(from string) {
			for _, to := range calls[from] {
				if !reached[to] {
					reached[to] = true
					walk(to)
				}
			}
		}
		for _, start := range append(starts, "") {
			reached[start] = true // an entry point is reached by definition: the name
			walk(start)           // a provider's own test utters is outside this package
		}
		var stranded []string
		for _, name := range calls["*"] {
			if !reached[name] {
				stranded = append(stranded, name)
			}
		}
		if len(stranded) > 0 {
			slices.Sort(stranded)
			t.Errorf("%s declares %d function(s) no entry point reaches: %s",
				dir, len(stranded), strings.Join(stranded, ", "))
		}
	}
}

// suiteCalls parses dir as one package and returns, under "*", every top-level
// function it declares, and under a declaration's own name the declared functions
// its body or initialiser mentions — "" being the package, whose tables run at
// init. It also returns the names a caller or this package's own test can start.
// Methods are reached through their receiver rather than by name, so they are
// neither declared nor followed here.
func suiteCalls(t *testing.T, dir string) (calls map[string][]string, starts []string) {
	t.Helper()
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	fset, files := token.NewFileSet(), []*ast.File{}
	for _, entry := range dirEntries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, entry.Name()), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", filepath.Join(dir, entry.Name()), err)
		}
		files = append(files, file)
	}
	declared, calls := map[string]bool{}, map[string][]string{}
	seen := map[string]bool{}
	for _, file := range files {
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil {
				declared[fn.Name.Name] = true
				if !seen[fn.Name.Name] {
					seen[fn.Name.Name] = true
					calls["*"] = append(calls["*"], fn.Name.Name)
				}
			}
		}
	}
	add := func(from, to string) { calls[from] = append(calls[from], to) }
	for _, file := range files {
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil && d.Body != nil {
					from := d.Name.Name
					if strings.HasPrefix(from, "Test") || ast.IsExported(from) {
						starts = append(starts, from)
					}
					ast.Inspect(d.Body, func(n ast.Node) bool {
						if id, ok := n.(*ast.Ident); ok && declared[id.Name] && id.Name != from {
							add(from, id.Name)
						}
						return true
					})
				}
			case *ast.GenDecl:
				ast.Inspect(d, func(n ast.Node) bool {
					if id, ok := n.(*ast.Ident); ok && declared[id.Name] {
						add("", id.Name)
					}
					return true
				})
			}
		}
	}
	slices.Sort(starts)
	return calls, slices.Compact(starts)
}
