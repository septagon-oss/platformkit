package ui_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"testing"
)

// TestEveryDoorGuardRefusalIsWrittenWhereTheAdviceCheckReadsIt asks that each refusal of the served design
// floor's door guards carry its message as a string literal, or as literals joined with +, the only shape the
// advice check beside this one reads.
//
// That check parses the first argument of each t.Error, t.Errorf, t.Fatal and t.Fatalf in these guards and asks
// that every Go name a message advise toward be a declaration of this package, so a helper renamed with its call
// sites carried along is refused the day one of these messages is left pointing at the old name. It can only ask
// that of text it can read: a message held in a variable or a constant, returned by a call or joined at run time
// is skipped without a word, and the names inside it stop being asked about while every check stays green — the
// dead name travels unheard, which is the one thing this family exists to stop. So the reach of that guard is a
// property of how these refusals are written, so this case holds the property rather than the advice: each
// refusal of each guard is counted, one whose message no literal carries, or whose literal carries no words,
// is named with the file and line that holds it, and any guard that reports no refusal at all is refused,
// because a listing of guards none of which speaks leaves the check beside this one agreeing with nothing.
// Inputs: the guard checks the advice case itself lists, read only, and the same literal rule it uses there.
func TestEveryDoorGuardRefusalIsWrittenWhereTheAdviceCheckReadsIt(t *testing.T) {
	for _, guard := range doorGuardChecks(t) {
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, guard, nil, 0)
		if err != nil {
			t.Fatalf("read the refusals of %s: %v", guard, err)
		}
		refusals := 0
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !slices.Contains([]string{"Error", "Errorf", "Fatal", "Fatalf"}, sel.Sel.Name) || len(call.Args) == 0 {
				return true
			}
			if who, ok := sel.X.(*ast.Ident); !ok || who.Name != "t" {
				return true
			}
			refusals++
			if text, carried := joinedLiteral(call.Args[0]); !carried || text == "" {
				t.Errorf("%s reports a refusal at %s whose message no literal carries, or whose literal carries no words, so the check that asks whether the names in it still exist reads nothing there and goes on passing over advice no declaration of this package answers: write the sentence as a literal, or as literals joined with +",
					guard, set.Position(call.Pos()))
			}
			return true
		})
		if refusals == 0 {
			t.Errorf("%s stands in the listing of guards the advice check reads and reports no refusal, so nothing it confines ever reaches an author as prose", guard)
		}
	}
}
