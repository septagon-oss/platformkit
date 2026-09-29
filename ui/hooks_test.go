package ui

// The completeness pins behind the client gate. renderedHooks and
// components.Hooks are the vocabulary refuseClientSheet refuses a consumer rule
// for naming, and a gate is only as wide as the list it reads: an attribute the
// kernel renders that the list omits is a client rule in the last layer above
// every kernel rule for that element, and a name the list carries that nothing
// renders refuses a consumer for nothing. components' own test runs the same
// check on its own package; these run it on the rest of the kernel's markup and
// on the reader that decides which name a selector names.

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/components"
)

// renderedHookRE matches an attribute name in the data- namespace, however it is
// written: a quoted attribute in a template, a name built by concatenation, or
// the name in the sentence that explains one.
var renderedHookRE = regexp.MustCompile(`data-[a-z0-9]+(?:-[a-z0-9]+)*`)

// kernelMarkupDirs are the packages inside ui/ — outside ui/components — whose
// sources render markup the kernel owns: the page shell, the screens it
// generates, the gallery. Each imports ui rather than the other way round, which
// is why their vocabulary is declared in renderedHooks instead of exported by
// them. The check runs with ui/ as its directory, so a package is named by the
// last element of its path and renderedHooks' repo-relative file names lose
// their ui/ prefix to be read.
var kernelMarkupDirs = []string{"document", "page", "resource", "export"}

func TestEveryKernelHookRenderedOutsideComponentsIsListed(t *testing.T) {
	t.Parallel()
	for _, dir := range kernelMarkupDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			body, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			for _, hook := range renderedHookRE.FindAllString(string(body), -1) {
				if _, listed := kernelHooks[hook]; !listed {
					t.Errorf("%s is rendered by %s/%s but listed by no hook vocabulary, so a client rule may name it and the client layer carries it past every kernel rule", hook, dir, name)
				}
			}
		}
	}
}

// TestRenderedHooksNameTheFileThatRendersThem is the other half: the attribution
// in renderedHooks is a claim about a file, so a file has to carry the name.
// Without this the comment would rot into a name nobody renders, and a consumer
// rule would be refused for a hook the kernel dropped.
func TestRenderedHooksNameTheFileThatRendersThem(t *testing.T) {
	t.Parallel()
	for hook, file := range renderedHooks {
		body, err := os.ReadFile(strings.TrimPrefix(file, "ui/"))
		if err != nil {
			t.Errorf("renderedHooks attributes %s to %s, which this check cannot read: %v", hook, file, err)
			continue
		}
		if !strings.Contains(string(body), hook) {
			t.Errorf("renderedHooks says %s renders %s, and that file does not name it", file, hook)
		}
		if slices.Contains(components.Hooks, hook) {
			t.Errorf("%s is listed twice: once in components.Hooks and once in renderedHooks", hook)
		}
	}
}

// TestAttrNamesReadsTheNameASelectorAddresses pins the reader: one attribute name
// has several spellings a browser resolves to it, and the gate compares names,
// not bytes.
func TestAttrNamesReadsTheNameASelectorAddresses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ selector, want string }{
		{"[data-component=button]", "data-component"},
		{"[ data-component=button ]", "data-component"},
		{"[data-component = button ]", "data-component"},
		{"[DATA-COMPONENT=button]", "data-component"},
		{`[data-component="button"]`, "data-component"},
		{"[data-component='button']", "data-component"},
		{"[data-component=button]:has(> span)", "data-component"},
		{`[\64 ata-component="button"]`, "data-component"},
		{`[data\2D component="button"]`, "data-component"},
		{`[\44 ATA-COMPONENT=button]`, "data-component"},
		{"[data-component]", "data-component"},
		{"[data-size~=\"lg\"]", "data-size"},
		{"[data-tone|=\"warm\"]", "data-tone"},
		{"[data-tone^=\"warm\"]", "data-tone"},
		{"[data-principal][data-signin]", "data-principal,data-signin"},
		{`[*|data-tone="warm"]`, "data-tone"},
		{`[ns|data-tone="warm"]`, "data-tone"},
		{`[data-x="]"]`, "data-x"},
		{":not([open])", "open"},
		{".flex", ""},
		{"from", ""},
		{"[data-a", "data-a"},
		{"[=x]", ""},
	} {
		if got := strings.Join(attrNames(tc.selector), ","); got != tc.want {
			t.Errorf("attrNames(%q) = %q, want %q", tc.selector, got, tc.want)
		}
	}
}
