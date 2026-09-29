package components_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/components"
)

// hookNameRE matches an attribute name in the data- namespace, however it is
// written: a quoted attribute in a template, a name built by concatenation, or
// a name in the sentence that explains one.
var hookNameRE = regexp.MustCompile(`data-[a-z0-9]+(?:-[a-z0-9]+)*`)

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
		for _, found := range hookNameRE.FindAllString(string(body), -1) {
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
