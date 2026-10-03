package page

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

func TestRefusalCataloguesHaveMatchingKeys(t *testing.T) {
	catalogueKeys := func(language string) map[string]json.RawMessage {
		t.Helper()
		body, err := catalogues.ReadFile("messages/" + language + ".json")
		if err != nil {
			t.Fatalf("read the %s refusal catalogue: %v", language, err)
		}
		var keys map[string]json.RawMessage
		if err := json.Unmarshal(body, &keys); err != nil {
			t.Fatalf("parse the %s refusal catalogue: %v", language, err)
		}
		return keys
	}

	english := catalogueKeys("en")
	portuguese := catalogueKeys("pt-PT")
	for key := range portuguese {
		if _, ok := english[key]; !ok {
			t.Errorf("Portuguese refusal key %q is absent in English", key)
		}
	}
	for key := range english {
		if _, ok := portuguese[key]; !ok {
			t.Errorf("English refusal key %q is absent in Portuguese", key)
		}
	}
}

func TestRefusalLabelsLiveInCatalogues(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "fault.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]bool{
		"Try again":                             true,
		"This should work again in %d seconds.": true,
		"Go back to the page you came from":     true,
		"Sign in":                               true,
		"Request reference":                     true,
	}
	ast.Inspect(file, func(node ast.Node) bool {
		literal, ok := node.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(literal.Value)
		if err != nil {
			t.Fatal(err)
		}
		if labels[value] {
			t.Errorf("user-facing label %q is a literal in ui/page/fault.go instead of catalogue copy", value)
		}
		return true
	})
}
