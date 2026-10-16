package resource

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// raisedIn returns every `screens.` key the Go source of one package asks for.
//
// The parser is the point: the alternative is a list somebody maintains beside the
// renderers, which is a second source of the same fact and goes wrong exactly when
// a screen gains a label — the case this gate exists to catch. Test files are left
// out because a key a test invents is not a key a person is ever shown.
func raisedIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("%s cannot be read: %v", dir, err)
	}
	set := map[string]bool{}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("%s/%s cannot be parsed: %v", dir, name, err)
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			literal, ok := n.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			if text, err := strconv.Unquote(literal.Value); err == nil && strings.HasPrefix(text, "screens.") {
				set[text] = true
			}
			return true
		})
	}
	raised := make([]string, 0, len(set))
	for key := range set {
		raised = append(raised, key)
	}
	slices.Sort(raised)
	return raised
}

// cataloguedIn returns the keys one embedded catalogue directory carries, in every
// locale it ships. The `screens.` vocabulary is the words a generated screen says,
// and both of the packages that write those screens are read: ui/screens mounts the
// forms whose titles come from the same keys.
func cataloguedIn(t *testing.T) []string {
	t.Helper()
	locales, err := fs.Glob(Catalogues(), "*.json")
	if err != nil || len(locales) == 0 {
		t.Fatalf("no catalogues to check: %v", err)
	}
	set := map[string]bool{}
	for _, locale := range locales {
		body, err := fs.ReadFile(Catalogues(), locale)
		if err != nil {
			t.Fatalf("%s cannot be read: %v", locale, err)
		}
		var messages map[string]json.RawMessage
		if err := json.Unmarshal(body, &messages); err != nil {
			t.Fatalf("%s is not gotext JSON: %v", locale, err)
		}
		for key := range messages {
			set[key] = true
		}
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// Every word a generated screen says is in a catalogue, and nothing in a catalogue
// is a word no screen says. The first direction is the one a person meets: a screen
// that asks for a key no locale answers keeps its English while the rest of the
// page translated, which reads as a bug and is a missing file. The second is the
// same drift on the other side — copy nobody renders, which is stale before anyone
// notices and is deleted by nobody.
func TestTheScreenVocabularyAndTheCatalogueAreOneList(t *testing.T) {
	t.Parallel()
	raised := append(raisedIn(t, "."), raisedIn(t, filepath.Join("..", "screens"))...)
	slices.Sort(raised)
	raised = slices.Compact(raised)
	catalogued := cataloguedIn(t)

	for _, key := range raised {
		if !slices.Contains(catalogued, key) {
			t.Errorf("%q is raised by a generated screen and carried by no catalogue, so one language of the screen is English and the other is not", key)
		}
	}
	for _, key := range catalogued {
		if !slices.Contains(raised, key) {
			t.Errorf("%q is carried by a catalogue no generated screen ever asks for", key)
		}
	}
	if len(raised) == 0 {
		t.Fatal("no screen key was found at all, which means this gate stopped looking")
	}
}
