package ui_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestEveryDoorGuardRefusalNamesACallThatStillExists asks that every Go name the served design floor's door
// guards advise a refused author toward is declared in this package.
//
// These guards refuse in prose: the message names the helper a section is cut through, the pattern a door is
// named by and the check that compares the two. A name inside a message is invisible to the compiler, so the day
// a helper is renamed and its calls updated the refusal goes on pointing at a call nothing declares, and the
// author it stops is told to do something the package no longer offers. Inputs: the checks of this directory
// whose names begin served_floor_ and hold door, read only, and the package-level declarations and methods of
// every source and check of this directory. A message naming nothing is left alone, a name behind a dot is asked
// of what the dot reaches rather than of the word after it, and a name no declaration answers is refused with
// the file, the message and the name.
func TestEveryDoorGuardRefusalNamesACallThatStillExists(t *testing.T) {
	declared := declaredHere(t)
	for _, guard := range doorGuardChecks(t) {
		for _, advice := range refusalAdvice(t, guard) {
			for _, name := range advisedNames(advice) {
				if !declared[name] {
					t.Errorf("%s refuses with %q, which points at %q that no declaration of this package answers: the message is the only instruction a refused author gets, so a name in it must exist — carry the refusal along with the helper it names", guard, advice, name)
				}
			}
		}
	}
}

// doorGuardChecks lists the checks of this package that refuse in prose about the served design floor's doors.
func doorGuardChecks(t *testing.T) []string {
	names, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatalf("list the checks of this package: %v", err)
	}
	var out []string
	for _, name := range names {
		if strings.HasPrefix(filepath.Base(name), "served_floor_") && strings.Contains(name, "door") {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		t.Fatal("this package holds no guard of the served design floor's doors, so no refusal names a call here")
	}
	return out
}

// refusalAdvice returns the first argument of every refusal these checks report — the message a person reads,
// whether one literal or literals joined with +. A file that cannot be read or parsed is refused, because then
// none of its advice could have been read at all.
func refusalAdvice(t *testing.T, path string) []string {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("read the refusals of %s: %v", path, err)
	}
	var out []string
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !slices.Contains([]string{"Error", "Errorf", "Fatal", "Fatalf"}, sel.Sel.Name) {
			return true
		}
		if who, ok := sel.X.(*ast.Ident); !ok || who.Name != "t" || len(call.Args) == 0 {
			return true
		}
		if text, ok := joinedLiteral(call.Args[0]); ok && text != "" {
			out = append(out, text)
		}
		return true
	})
	return out
}

// joinedLiteral returns the text of an expression written as a string literal, or as literals joined with +.
func joinedLiteral(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		if text, err := strconv.Unquote(e.Value); err == nil {
			return text, true
		}
		return strings.Trim(e.Value, "`"), true
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		left, okLeft := joinedLiteral(e.X)
		right, okRight := joinedLiteral(e.Y)
		return left + right, okLeft && okRight
	}
	return "", false
}

// goWrittenName matches a word written the way Go writes a name, so that the guide's own all-capital name and
// the format verbs of a message are never taken for one.
var goWrittenName = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_]*`)

// advisedNames lists the names a message writes as Go writes a name: mixed case, and not the member of a dotted
// pair, which the text before the dot already answers for.
func advisedNames(message string) []string {
	var out []string
	for _, at := range goWrittenName.FindAllStringIndex(message, -1) {
		name := message[at[0]:at[1]]
		if at[0] > 0 && message[at[0]-1] == '.' {
			continue
		}
		if strings.ToLower(name) == name || strings.ToUpper(name) == name {
			continue
		}
		out = append(out, name)
	}
	return out
}

// declaredHere names every package-level declaration of this directory, methods included, since a refusal may
// point at a method of a type beside it. An unreadable source is refused: the guard walks the same listing.
func declaredHere(t *testing.T) map[string]bool {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("list the sources of this package: %v", err)
	}
	declared := map[string]bool{}
	for _, name := range files {
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatalf("read the declarations of %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				declared[d.Name.Name] = true
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.ValueSpec:
						for _, ident := range s.Names {
							declared[ident.Name] = true
						}
					case *ast.TypeSpec:
						declared[s.Name.Name] = true
					}
				}
			}
		}
	}
	return declared
}
