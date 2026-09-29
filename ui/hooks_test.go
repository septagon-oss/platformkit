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

// renderedHookRE matches an attribute name in the data- namespace, however the
// markup is written: a quoted attribute in a template, a name built by
// concatenation, or a name inside a raw markup string. It runs on source with
// its comments blanked — renderedHookNames — because a name in the sentence that
// explains one is not a name the package renders.
var renderedHookRE = regexp.MustCompile(`data-[a-z0-9]+(?:-[a-z0-9]+)*`)

// renderedHookNames returns the data- attribute names src mentions outside its
// comments. go/parser decides what a comment is; it is what tells a // inside a
// string from a comment that starts with one, which a text search cannot. A
// source it cannot parse fails the check: read raw it would report prose as
// markup, and skipped it would miss real markup, and neither answers the question.
func renderedHookNames(t *testing.T, src []byte) []string {
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
	return renderedHookRE.FindAllString(string(code), -1)
}

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
			for _, hook := range renderedHookNames(t, body) {
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
		if !slices.Contains(renderedHookNames(t, body), hook) {
			t.Errorf("renderedHooks says %s renders %s, and that file names it nowhere but a comment", file, hook)
		}
		if slices.Contains(components.Hooks, hook) {
			t.Errorf("%s is listed twice: once in components.Hooks and once in renderedHooks", hook)
		}
	}
}

// TestANameInACommentIsNotAHookTheKernelRenders pins the scope both completeness
// checks run in. A scan that read prose made them a text search: a later branch
// that mentions htmx's data-hx-boost in a sentence fails with "is rendered by
// page/render.go", which is false of it, and the only fix inside that branch is
// to add a third party's attribute to the kernel's vocabulary — which would then
// refuse every client's legitimate use of that namespace. Markup still counts,
// in every form markup is written here.
func TestANameInACommentIsNotAHookTheKernelRenders(t *testing.T) {
	t.Parallel()
	src := []byte("package page\n\n" +
		"// The controller reads data-hx-boost and swaps the region.\n" +
		"func render(g *html.G) { g.Attr(\"data-theme\", \"dark\") } // data-hx-swap\n" +
		"/* data-hx-confirm */\n" +
		"const shell = `<div data-gallery-status=ok>`\n")
	got := strings.Join(renderedHookNames(t, src), ",")
	if want := "data-theme,data-gallery-status"; got != want {
		t.Errorf("names read outside comments = %q, want %q: a name in a comment is not markup and a name in a template string is", got, want)
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
