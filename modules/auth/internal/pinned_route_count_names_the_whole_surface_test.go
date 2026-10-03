package internal_test

// The claim that stopped the brief, pinned: the route count names the whole surface.
//
// IMPLEMENT.md ("What is still open") says the module's HTTP surface "is frozen
// at eleven routes by one test file": because
// route_list_reads_as_a_declaration_test.go fails unless handler.go
// registers exactly the number it names, and its failure text calls that list
// "the module's whole HTTP surface", the case concluded that the brief's item 2
// (a second factor: "Item 2 needs at least four new registrations") and item 4
// (bearer tokens: "item 4 needs three") could not reach a caller from inside a
// delivery round at all.
//
// This case reads the module instead of the claim. Two numbers:
//
// - the integer the route pin compares `registrations` against, read out of
// the pin's own syntax tree, so this case cannot drift from it;
// - every httpx.Register call expression in every non-test file of this
// package, which is what this module actually answers with.
//
// They are asserted equal, because the pin's sentence says the list it reads is
// the whole surface. While they differ, the operations outside handler.go are
// invisible to that pin — so the surface the pin promises to guard is not the
// surface the module has, and a factor's or a token's route written beside
// email_registration.go, oidc.go, registration.go or approval_registration.go,
// which is how this module already routes seven of its operations, leaves the
// pin green. That is a fact about the tree, reached by parsing it: no refusal,
// no redirect and no sentence any defect prints is part of the assertion.
//
// It turns green without editing this case under either honest cure: the
// registrations all answer from the list the pin reads, or the pin is
// re-recorded to name the whole surface it claims to name. It stays
// red under the dishonest one — new routes in a second file with the number left
// where it was.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// routePinFile is the file whose recorded count is under test here.
const routePinFile = "route_list_reads_as_a_declaration_test.go"

func TestThePinnedRouteCountNamesEveryOperationTheModuleRegisters(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	total := 0
	var outside, all []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		counted := 0
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if ok && pkg.Name == "httpx" && sel.Sel.Name == "Register" {
				counted++
			}
			return true
		})
		if counted == 0 {
			continue
		}
		total += counted
		all = append(all, name+" "+strconv.Itoa(counted))
		if name != "handler.go" {
			outside = append(outside, name+" "+strconv.Itoa(counted))
		}
	}
	sort.Strings(all)

	named, found := pinnedRouteCount(t, fset)
	if !found {
		// Root adopts (decision 0008, 2026-10-01): this pin names no count any more, because the module's
		// whole surface is surface_test.go's mount-record table. With no count pinned, no count can name
		// less than the surface, which is this case's whole question.
		return
	}
	if named == total {
		return
	}
	t.Fatalf("%s refuses the list unless handler.go registers exactly %d operations, and calls that list "+
		"\"the module's whole HTTP surface\"; this module registers %d operations, in %s. The %d registered "+
		"outside handler.go (%s) are outside that pin's reach, so the sentence is false for them and a new "+
		"operation routed in one of those files leaves the pin green — which is the reason this branch gave "+
		"for not mounting the brief's second factor and bearer token. Declare the whole surface where the "+
		"pin reads it, or re-record the pin over the whole surface.",
		routePinFile, named, total, strings.Join(all, ", "), total-countedIn(all, "handler.go"),
		strings.Join(outside, ", "))
}

// countedIn is the count one file contributed, 0 when it contributed none.
func countedIn(perFile []string, name string) int {
	for _, entry := range perFile {
		if file, count, ok := strings.Cut(entry, " "); ok && file == name {
			n, err := strconv.Atoi(count)
			if err != nil {
				return 0
			}
			return n
		}
	}
	return 0
}

// pinnedRouteCount is the integer the route pin compares `registrations`
// against, read from its syntax tree rather than from a copy of its text.
func pinnedRouteCount(t *testing.T, fset *token.FileSet) (int, bool) {
	t.Helper()
	file, err := parser.ParseFile(fset, routePinFile, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", routePinFile, err)
	}
	var (
		pinned int
		found  bool
	)
	ast.Inspect(file, func(n ast.Node) bool {
		binary, ok := n.(*ast.BinaryExpr)
		if !ok || binary.Op != token.NEQ {
			return true
		}
		left, ok := binary.X.(*ast.Ident)
		if !ok || left.Name != "registrations" {
			return true
		}
		literal, ok := binary.Y.(*ast.BasicLit)
		if !ok {
			return true
		}
		value, err := strconv.Atoi(literal.Value)
		if err != nil {
			return true
		}
		pinned, found = value, true
		return true
	})
	return pinned, found
}
