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

// TestEveryRuleNameARefusalLiteralCarriesPrintsItsOwnAdvice requires that every rule
// name this package's production source hands to a refusal prints the non-empty advice
// the remedy map holds for it, whichever way the literal carrying that name is written:
// fields in order, where the rule is the second element, or keyed, where it sits under
// rule, whether the literal names its type or stands as an element of a []refusal. An
// author's choice of shape is not a rule's business: one reachable only through a shape
// nothing reads prints a refusal with nothing after the pair, and the ordered reads
// never see it. Inputs: this package's non-test source and one synthetic pair per rule,
// since the advice depends on the rule and on nothing else. Failure: a name in any
// refusal literal whose printed line carries no advice, or an advice that differs from
// the map's.
func TestEveryRuleNameARefusalLiteralCarriesPrintsItsOwnAdvice(t *testing.T) {
	rules := literalRuleNames(t)
	if len(rules) == 0 {
		t.Fatal("this package's source writes no rule name into a refusal literal, so this read reaches nothing to check")
	}
	owner, target := foundation+"/modules/billing/contracts", foundation+"/modules/ledger/contracts"
	for _, rule := range rules {
		line, _, _ := strings.Cut(diagnostics([]refusal{{
			edge: edge{from: owner, to: target, ownerPath: owner, context: "production"},
			rule: rule,
		}}), "\n")
		_, advice, separated := strings.Cut(line, "): ")
		if want := remedies[rule]; !separated || advice != want || strings.TrimSpace(want) == "" {
			t.Errorf("the refusal literal naming %q must print the remedy %q beside the pair, printed %q", rule, remedies[rule], line)
		}
	}
}

// literalRuleNames returns, sorted and without repeats, every rule name written into a
// refusal in this package's non-test source, in any of the literal shapes above. A Go
// test runs in its own package directory, so the read stays inside this package and
// names no figure from outside it.
func literalRuleNames(t *testing.T) []string {
	t.Helper()
	seen := map[string]bool{}
	ruleName := func(expr ast.Expr) {
		lit, ok := expr.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return
		}
		if rule, err := strconv.Unquote(lit.Value); err == nil && rule != "" {
			seen[rule] = true
		}
	}
	// fields reads one refusal's rule out of its own elements: the second one where the
	// fields stand in order, the value under rule where they are keyed.
	fields := func(lit *ast.CompositeLit) {
		for at, field := range lit.Elts {
			if pair, keyed := field.(*ast.KeyValueExpr); keyed {
				if key, named := pair.Key.(*ast.Ident); named && key.Name == "rule" {
					ruleName(pair.Value)
				}
			} else if at == 1 {
				ruleName(field)
			}
		}
	}
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return strings.HasSuffix(fi.Name(), ".go") && !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("this package's source cannot be parsed: %v", err)
	}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(node ast.Node) bool {
				lit, ok := node.(*ast.CompositeLit)
				if !ok {
					return true
				}
				switch typed := lit.Type.(type) {
				case *ast.Ident: // refusal{edge, "rule", detail} or refusal{rule: "rule"}
					if typed.Name == "refusal" {
						fields(lit)
					}
				case *ast.ArrayType: // []refusal{{rule: "rule"}, …}, each element naming no type
					if name, named := typed.Elt.(*ast.Ident); named && name.Name == "refusal" {
						for _, element := range lit.Elts {
							if element, untyped := element.(*ast.CompositeLit); untyped && element.Type == nil {
								fields(element)
							}
						}
					}
				}
				return true
			})
		}
	}
	return slices.Sorted(maps.Keys(seen))
}
