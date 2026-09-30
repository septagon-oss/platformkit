package components_test

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/components"
)

// hookNameRE matches an attribute name in the data- namespace, however the
// markup is written: a quoted attribute in a template, a name built by
// concatenation, or a name inside a raw markup string. It runs on source with its
// comments blanked — hookNames — because a name in the sentence that explains one
// is not a name this package renders.
var hookNameRE = regexp.MustCompile(`data-[a-z0-9]+(?:-[a-z0-9]+)*`)

// hookNames returns the data- attribute names src mentions outside its
// comments. go/parser decides what a comment is; it is what tells a // inside a
// string from a comment that starts with one, which a text search cannot. A
// source it cannot parse fails the check rather than being read as prose.
func hookNames(t *testing.T, src []byte) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "check.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v: this check reads markup, and a file it cannot parse, it cannot scope to markup", err)
	}
	code := bytes.Clone(src)
	for _, group := range file.Comments {
		for _, comment := range group.List {
			for i := fset.Position(comment.Pos()).Offset; i < fset.Position(comment.End()).Offset; i++ {
				if code[i] != '\n' {
					code[i] = ' '
				}
			}
		}
	}
	return hookNameRE.FindAllString(string(code), -1)
}

// TestANameInACommentIsNotAHookThisPackageRenders pins the scope of the check
// above. A scan that read prose made it a text search: a branch that mentions
// htmx's data-hx-boost in a sentence fails "is rendered by this package", and the
// only fix inside that branch is to widen components.Hooks — the vocabulary a
// client rule is refused for naming — with a third party's attribute. Markup
// counts, in every form markup is written here.
func TestANameInACommentIsNotAHookThisPackageRenders(t *testing.T) {
	t.Parallel()
	src := []byte("package components\n\n" +
		"// The controller reads data-hx-boost and swaps the region.\n" +
		"func render(g *html.G) { g.Attr(\"data-component\", \"modal\") } // data-hx-swap\n" +
		"/* data-hx-confirm */\n" +
		"const markup = `<span data-tone=cool>`\n")
	got := strings.Join(hookNames(t, src), ",")
	if want := "data-component,data-tone"; got != want {
		t.Errorf("names read outside comments = %q, want %q: a name in a comment is not markup and a name in a template string is", got, want)
	}
}

// TestEveryRenderedHookIsListed is the completeness pin behind components.Hooks.
// The list is what ui refuses a client rule for naming, so an unlisted hook is a
// hole in that gate and a stale name is a name consumers are refused for no
// reason. The case reads the package's own sources — the markup lives there, and
// nothing else in the build knows which attributes a component emits.
//
// hooks.go is the list under test and is not its own evidence; _test.go files are
// skipped because a test writes no markup a stylesheet addresses.
func TestEveryRenderedHookIsListed(t *testing.T) {
	t.Parallel()
	listed := map[string]bool{}
	for _, name := range components.Hooks {
		if listed[name] {
			t.Errorf("components.Hooks lists %s twice", name)
		}
		listed[name] = true
		if !strings.HasPrefix(name, "data-") || !hookNameRE.MatchString(name) {
			t.Errorf("components.Hooks carries %q, which is not a data- attribute name", name)
		}
	}
	if !slices.IsSorted(components.Hooks) {
		t.Error("components.Hooks is not sorted, so a reader cannot diff two revisions of it")
	}

	rendered := map[string]bool{}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || name == "hooks.go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, found := range hookNames(t, body) {
			rendered[found] = true
		}
	}
	if len(rendered) == 0 {
		t.Fatal("no data- attribute was read from the package sources, which is not how this package renders markup")
	}
	for name := range rendered {
		if !listed[name] {
			t.Errorf("%s is rendered by this package but missing from components.Hooks, so a client rule may name it", name)
		}
	}
	for name := range listed {
		if !rendered[name] {
			t.Errorf("%s is in components.Hooks but rendered nowhere in this package, so a client rule naming it is refused for nothing", name)
		}
	}
}
