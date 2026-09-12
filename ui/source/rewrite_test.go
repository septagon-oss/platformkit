package source

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func rewriteFixture(t *testing.T, body string) (dir, filename string, content []byte, line, column int) {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir = t.TempDir()
	module := "module example.com/source-fixture\n\ngo 1.26.6\n\nrequire (\n" +
		"github.com/septagon-oss/platformkit v0.0.0\nmaragu.dev/gomponents v1.3.0\n)\n" +
		"replace github.com/septagon-oss/platformkit => " + strconv.Quote(root) + "\n"
	sums, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	content = []byte("package fixture\nimport (\nc \"github.com/septagon-oss/platformkit/ui/components\"\ng \"maragu.dev/gomponents\"\n)\nvar _ g.Node\n" + body)
	for name, data := range map[string][]byte{"go.mod": []byte(module), "go.sum": sums, "fixture.go": content} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	filename = filepath.Join(dir, "fixture.go")
	position := bytes.Index(content, []byte("/*target*/")) + len("/*target*/")
	line = bytes.Count(content[:position], []byte("\n")) + 1
	column = position - bytes.LastIndexByte(content[:position], '\n')
	return
}

func TestRewriteLiteralKeepsCommentsImportsAndOtherCall(t *testing.T) {
	body := `var first = /*target*/c.ExampleOf(c.ExampleInfo{ID: "same"}, c.ButtonProps{
	// The product owner explains this label here.
	Label: "Save", // Keep this comment with the property.
}, c.Button)
var second = c.ExampleOf(c.ExampleInfo{ID: "same"}, c.ButtonProps{Label: "Save"}, c.Button)
`
	dir, filename, before, line, column := rewriteFixture(t, body)
	after, err := rewrite(t.Context(), dir, filename, before, line, column, json.RawMessage(`{"label":"Create & \"keep\""}`))
	if err != nil {
		t.Fatal(err)
	}
	text := string(after)
	for _, want := range []string{`Label: "Create & \"keep\"", // Keep this comment with the property.`, "// The product owner explains this label here.\n\tLabel:", `c "github.com/septagon-oss/platformkit/ui/components"`, `Label: "Save"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("rewritten file lost %q:\n%s", want, text)
		}
	}
	current, err := os.ReadFile(filename)
	if err != nil || !bytes.Equal(current, before) {
		t.Fatal("candidate generation wrote to the source file")
	}
}

func TestRewriteTypedCaptures(t *testing.T) {
	for _, tc := range []struct{ name, body, props, want string }{
		{"explicit-alias", `type Props = c.ButtonProps
var example = /*target*/c.ExampleOf[Props](c.ExampleInfo{}, Props{Label: "Before"}, c.Button)`, `{"label":"After"}`, `Label: "After"`},
		{"children", `var example = /*target*/c.ExampleWithChildren[c.StackProps](c.ExampleInfo{}, c.StackProps{Gap: "4"}, []g.Node{}, c.Stack)`, `{"gap":"6"}`, `Gap: "6"`},
		{"slots", `var example = /*target*/c.ExampleWithSlots[c.ButtonProps, c.ButtonSlots](c.ExampleInfo{}, c.ButtonProps{Label: "Before"}, c.ButtonSlots{}, c.ButtonWithSlots)`, `{"label":"After"}`, `Label: "After"`},
		{"defined-string", "type Copy string\ntype Props struct { Text Copy `json:\"copy\"` }\n" +
			`var example = /*target*/c.ExampleOf(c.ExampleInfo{}, Props{Text: "Before"}, func(p Props) g.Node { return g.Text(string(p.Text)) })`, `{"copy":"After"}`, `Text: "After"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, filename, before, line, _ := rewriteFixture(t, tc.body)
			after, err := rewrite(t.Context(), dir, filename, before, line, 0, json.RawMessage(tc.props))
			if err != nil || !strings.Contains(string(after), tc.want) {
				t.Fatalf("rewrite: %v\n%s", err, after)
			}
		})
	}
}

func TestRewriteColumnDisambiguatesCallsOnOneLine(t *testing.T) {
	capture := `c.ExampleOf(c.ExampleInfo{}, c.ButtonProps{Label: "Before"}, c.Button)`
	dir, filename, before, line, column := rewriteFixture(t, `var first, second = /*target*/`+capture+`, `+capture)
	patch := json.RawMessage(`{"label":"After"}`)
	after, err := rewrite(t.Context(), dir, filename, before, line, column, patch)
	if err != nil || strings.Count(string(after), `Label: "After"`) != 1 || strings.Count(string(after), `Label: "Before"`) != 1 {
		t.Fatalf("column did not select exactly one call: %v\n%s", err, after)
	}
	if result, err := rewrite(t.Context(), dir, filename, before, line, column+1, patch); err == nil || result != nil {
		t.Fatalf("column inside a call was accepted: %v", err)
	}
}

func TestRewriteRefusesUnsupportedSource(t *testing.T) {
	capture := `c.ExampleOf(c.ExampleInfo{}, c.ButtonProps{Label: "Before"}, c.Button)`
	for _, tc := range []struct{ name, body, props, want string }{
		{"same-name-function", `func ExampleOf(c.ExampleInfo, c.ButtonProps, func(c.ButtonProps) g.Node) g.Node { return nil }
var example = /*target*/ExampleOf(c.ExampleInfo{}, c.ButtonProps{Label: "Before"}, c.Button)`, `{"label":"After"}`, "selects 0"},
		{"ambiguous-line", `var first, second = /*target*/` + capture + `, ` + capture, `{"label":"After"}`, "selects 2"},
		{"computed", `var example = /*target*/c.ExampleOf(c.ExampleInfo{}, c.ButtonProps{Label: "Be" + "fore"}, c.Button)`, `{"label":"After"}`, "missing or computed"},
		{"missing-literal", `var example = /*target*/c.ExampleOf(c.ExampleInfo{}, c.ButtonProps{}, c.Button)`, `{"label":"After"}`, "missing or computed"},
		{"indirect-props", `var props = c.ButtonProps{Label: "Before"}
var example = /*target*/c.ExampleOf(c.ExampleInfo{}, props, c.Button)`, `{"label":"After"}`, "direct keyed"},
		{"wrong-json-case", `var example = /*target*/` + capture, `{"Label":"After"}`, "not a direct exported"},
		{"promoted-field", `var example = /*target*/` + capture, `{"id":"After"}`, "not a direct exported"},
		{"nonstring-field", `var example = /*target*/` + capture, `{"loading":"true"}`, "not a string field"},
		{"internal-field", "type Props struct { Secret string `json:\"-\"` }\n" +
			`var example = /*target*/c.ExampleOf(c.ExampleInfo{}, Props{Secret: "Before"}, func(Props) g.Node { return nil })`, `{"Secret":"After"}`, "not a direct exported"},
		{"internal-delivery", "type Props struct { Secret string `json:\"secret\" delivery:\"internal\"` }\n" +
			`var example = /*target*/c.ExampleOf(c.ExampleInfo{}, Props{Secret: "Before"}, func(Props) g.Node { return nil })`, `{"secret":"After"}`, "not a direct exported"},
		{"private-field", "type Props struct { secret string `json:\"secret\"` }\n" +
			`var example = /*target*/c.ExampleOf(c.ExampleInfo{}, Props{secret: "Before"}, func(Props) g.Node { return nil })`, `{"secret":"After"}`, "not a direct exported"},
		{"duplicate-json-tag", "type Props struct { First string `json:\"text\"`; Second string `json:\"text\"` }\n" +
			`var example = /*target*/c.ExampleOf(c.ExampleInfo{}, Props{First: "A", Second: "B"}, func(Props) g.Node { return nil })`, `{"text":"After"}`, "ambiguous Go fields"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, filename, before, line, _ := rewriteFixture(t, tc.body)
			after, err := rewrite(t.Context(), dir, filename, before, line, 0, json.RawMessage(tc.props))
			if err == nil || !strings.Contains(err.Error(), tc.want) || after != nil {
				t.Fatalf("expected %q refusal: %v", tc.want, err)
			}
			current, err := os.ReadFile(filename)
			if err != nil || !bytes.Equal(current, before) {
				t.Fatal("refused edit changed source")
			}
		})
	}
}

func TestStringPatchRejectsNonstringAndAmbiguousJSON(t *testing.T) {
	for _, raw := range []string{`{}`, `null`, `[]`, `{"label":null}`, `{"label":42}`, `{"label":true}`, `{"label":[]}`, `{"label":{}}`, `{"label":"one","label":"two"}`, `{"label":"one"} {}`} {
		if patch, err := stringPatch(json.RawMessage(raw)); err == nil || patch != nil {
			t.Errorf("accepted %s: %v", raw, patch)
		}
	}
}

func TestCaptureRecognitionUsesResolvedFunctions(t *testing.T) {
	for _, expression := range []string{"ExampleOf()", "ExampleOf[P]()", "c.ExampleWithSlots[P, S]()"} {
		t.Run(expression, func(t *testing.T) {
			node, err := parser.ParseExpr(expression)
			if err != nil {
				t.Fatal(err)
			}
			var name *ast.Ident
			ast.Inspect(node, func(node ast.Node) bool {
				if id, ok := node.(*ast.Ident); ok && strings.HasPrefix(id.Name, "Example") {
					name = id
				}
				return true
			})
			for _, origin := range []string{capturePackage, "example.com/unrelated"} {
				object := types.NewFunc(token.NoPos, types.NewPackage(origin, "components"), name.Name, nil)
				pkg := &packages.Package{TypesInfo: &types.Info{Uses: map[*ast.Ident]types.Object{name: object}}}
				if isCapture(pkg, node.(*ast.CallExpr)) != (origin == capturePackage) {
					t.Fatalf("incorrect capture decision for %s", origin)
				}
			}
		})
	}
}
