package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestTheRunCarriesAnAdviceBesideEveryRuleItCanRefuseWith asks the refusal map the
// question one refusal cannot: the rules the gate can refuse with and the rules whose
// advice it carries have to be the same set, or a refusal prints a line with nothing a
// reader can act on. It collects the rule names this package's source hands to a
// refusal — a literal add call, the rule field of a refusal, or what the purity check
// returns — and requires a non-empty remedies entry for every one, and an emitted rule
// for every advice the map holds, so neither side drifts alone. It holds too that the
// advice for a module with no layer row names the map file this package reads, taken
// from layerFileSuffix rather than from the sentence, so moving that file cannot leave
// the advice naming a path that no longer exists. Inputs: this package's non-test
// source. Failure: a rule with no advice, an empty advice, an advice no rule prints.
func TestTheRunCarriesAnAdviceBesideEveryRuleItCanRefuseWith(t *testing.T) {
	rules := packageRuleNames(t)
	for _, rule := range rules {
		advice, ok := remedies[rule]
		if !ok {
			t.Errorf("the gate refuses with %q and remedies carries no advice for it, so that refusal prints no way out", rule)
			continue
		}
		if strings.TrimSpace(advice) == "" {
			t.Errorf("the advice for %q is empty, so that refusal prints no way out to act on", rule)
		}
	}
	for _, rule := range slices.Sorted(maps.Keys(remedies)) {
		if !slices.Contains(rules, rule) {
			t.Errorf("remedies carries advice for %q, which nothing in this package refuses with, so no line can print it", rule)
		}
	}
	mapFile := strings.TrimPrefix(layerFileSuffix, "/")
	if !strings.Contains(remedies["module-layer-missing"], mapFile) {
		t.Errorf("the advice for a module with no layer row does not name %q, the map this package reads: %q",
			mapFile, remedies["module-layer-missing"])
	}
}

// packageRuleNames reads this package's non-test source and returns, sorted, every
// rule name it can refuse with. A Go test runs in its own package directory, so the
// read stays inside this package: no other module's source and no whole-tree figure is
// involved.
func packageRuleNames(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return strings.HasSuffix(fi.Name(), ".go") && !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("this package's source cannot be parsed: %v", err)
	}
	rules := map[string]bool{}
	add := func(node ast.Expr) {
		lit, ok := node.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return
		}
		if rule, err := strconv.Unquote(lit.Value); err == nil && rule != "" {
			rules[rule] = true
		}
	}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(node ast.Node) bool {
				switch n := node.(type) {
				case *ast.CallExpr: // add("rule"), the shape every boundary check reports through
					if fn, ok := n.Fun.(*ast.Ident); ok && fn.Name == "add" && len(n.Args) == 1 {
						add(n.Args[0])
					}
				case *ast.CompositeLit: // refusal{edge, "rule", detail}
					if id, ok := n.Type.(*ast.Ident); ok && id.Name == "refusal" && len(n.Elts) >= 2 {
						add(n.Elts[1])
					}
				case *ast.FuncDecl: // purity names its two rules in what it returns
					if n.Name.Name == "purity" {
						ast.Inspect(n.Body, func(node ast.Node) bool {
							if r, ok := node.(*ast.ReturnStmt); ok && len(r.Results) == 1 {
								add(r.Results[0])
							}
							return true
						})
					}
				}
				return true
			})
		}
	}
	return slices.Sorted(maps.Keys(rules))
}
