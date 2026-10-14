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
	"github.com/septagon-oss/platformkit/ui/css"
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

// TestResolveNamesSpellsTextTheWayTheBrowserResolvesIt pins the read refuseClientSheet
// compares against, which was found to apply to attribute names and to nothing
// else. The cases that decide it: a hex escape's terminating whitespace belongs
// to the escape and does not reappear, a name is folded whatever case wrote it, and
// text with no escape in it comes back byte for byte — a read that rewrote ordinary
// text would refuse the sheets the gate is meant to take.
func TestResolveNamesSpellsTextTheWayTheBrowserResolvesIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ written, resolved string }{
		{":ROOT", ":root"},
		{`:ro\6f t`, ":root"},
		{`:\52 OOT`, ":root"},
		{`--\70 k-color-accent-default`, "--pk-color-accent-default"},
		{`--\70 K-color-accent-default`, "--pk-color-accent-default"},
		{`color: RGB(1, 2, 3)`, "color: rgb(1, 2, 3)"},
		{`#\36 6666`, "#66666"},
		{`color:#0f5d4e;outline-color`, "color:#0f5d4e;outline-color"},
		{`url("data:image/svg+xml,%3csvg%3e")`, `url("data:image/svg+xml,%3csvg%3e")`},
		{`[data\2D component]`, "[data-component]"},
		{`from; @layer tokens`, "from; @layer tokens"},
		// The escaped colon decodes to a colon. A browser reads `a\:root` as one
		// identifier, not as the root pseudo-class, and the gate refuses it anyway:
		// this is the wide direction resolveNames states it chooses.
		{`a\:root`, "a:root"},
		{`a\`, "a\\"},
	} {
		if got := resolveNames(tc.written); got != tc.resolved {
			t.Errorf("resolveNames(%q) = %q, want %q", tc.written, got, tc.resolved)
		}
	}
}

// TestClassNamesReadsTheClassASelectorAddresses pins the class half of the gate's
// read, and the one place it differs from attrNames: a browser decodes an escape
// wherever it stands, so `.\66 lex` is the kernel's own .flex and is refused; and
// an HTML class name is matched case-sensitively where an attribute name is not,
// so `.FLEX` names a class the kernel renders nowhere and the read keeps the case
// it was written with. Measured in this repository's own Chromium over the real
// composed sheet: class="flex" computes the kernel's rule under `.flex` and under
// `.\66 lex`, and its own under `.FLEX`.
func TestClassNamesReadsTheClassASelectorAddresses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ selector, want string }{
		{".store-hero", "store-hero"},
		{".flex.flex", "flex,flex"},
		{".store-hero, .sr-only", "store-hero,sr-only"},
		{".store-hero .card > .x + .y ~ .z", "store-hero,card,x,y,z"},
		{".text-fg-brand:hover", "text-fg-brand"},
		{":not(.sr-only)", "sr-only"},
		{`:is(.flex, .grid)`, "flex,grid"},
		{`.\66 lex`, "flex"},
		{`.\46 LEX`, "FLEX"},
		{".FLEX", "FLEX"},
		{`.gap-0\.5`, "gap-0.5"},
		{`.md\:flex`, "md:flex"},
		{`.focus\:z-\[1300\]:focus`, "focus:z-[1300]"},
		{`.space-y-1 > :not([hidden]) ~ :not([hidden])`, "space-y-1"},
		// A `.` inside a bracketed attribute or a quoted value is character data in
		// a token the browser already opened, and an escaped `.` is a name's own
		// character: neither addresses a class.
		{`[data-email="a.b"]`, ""},
		{`.store-card[aria-label*=".flex"]`, "store-card"},
		{`\2e store-hero`, ""},
		// What is not a class selector at all.
		{"dialog", ""},
		{"#pk-root", ""},
		{"from", ""},
		{"50%", ""},
		{":root", ""},
		{"", ""},
	} {
		if got := strings.Join(classNames(tc.selector), ","); got != tc.want {
			t.Errorf("classNames(%q) = %q, want %q", tc.selector, got, tc.want)
		}
	}
}

// TestAttrMatchesReadsWhetherASelectorComparesAValue pins the fact attrNames drops
// and the class-attribute refusal reads. A bracket states one of two things: that
// the element carries an attribute, or that its contents compare to something —
// and only the second puts a name the gate guards inside what the selector says,
// because a compared value is read as the attribute's data. Whitespace sits between
// a name and its operator and inside nothing; the six value comparisons are `=`,
// `~=`, `*=`, `^=`, `$=`, `|=`, and `!=` is no operator a browser reads.
func TestAttrMatchesReadsWhetherASelectorComparesAValue(t *testing.T) {
	t.Parallel()
	spell := func(matches []attrMatch) string {
		out := make([]string, 0, len(matches))
		for _, m := range matches {
			if m.value {
				out = append(out, m.name+"=value")
			} else {
				out = append(out, m.name+"=present")
			}
		}
		return strings.Join(out, ",")
	}
	for _, tc := range []struct{ selector, want string }{
		{`[class~="flex"]`, "class=value"},
		{`[class="flex"]`, "class=value"},
		{`[class*="flex"]`, "class=value"},
		{`[class^=flex]`, "class=value"},
		{`[class$="flex"]`, "class=value"},
		{`[class|="flex"]`, "class=value"},
		{`[ CLASS ~= "flex" ]`, "class=value"},
		{`[\63 lass~="flex"]`, "class=value"},
		{`[class~="a" i]`, "class=value"},
		{`[class]`, "class=present"},
		{`[ class ]`, "class=present"},
		{`[class!=x]`, "class=present"},
		{`[class ~ = "flex"]`, "class=present"},
		{`[data-store-panel]`, "data-store-panel=present"},
		{`[data-email="a.b"]`, "data-email=value"},
		{`.store-card[aria-label*=",flex"]`, "aria-label=value"},
		{`[*|data-tone="warm"]`, "data-tone=value"},
		{`[ns|class~="flex"]`, "class=value"},
		{`[data-a] [class~="flex"]`, "data-a=present,class=value"},
		{`[data-x="=]"] [class]`, "data-x=value,class=present"},
		{`.flex`, ""},
		{`[data-a`, "data-a=present"},
		{`[=x]`, ""},
	} {
		if got := spell(attrMatches(tc.selector)); got != tc.want {
			t.Errorf("attrMatches(%q) = %s, want %s", tc.selector, got, tc.want)
		}
	}
}

// TestEveryClassTheKernelEmitsIsInRefusedVocabulary is the completeness check
// kernelHooks gets from hooks_test.go and components.Hooks from components'
// own test, run on the class vocabulary: every class name in every selector the
// style engine resolves out of the lists ui/components declares — which is what
// ui.Compose emits into @layer components and ui.Gallery into gallery.css — is a
// name the gate refuses a client rule to write. A component that gains a class
// gains its refusal in the same change, and a name the vocabulary misses is a
// client rule in the last layer, above the kernel's own rule for that class.
func TestEveryClassTheKernelEmitsIsInRefusedVocabulary(t *testing.T) {
	t.Parallel()
	kernel := kernelClasses()
	if len(kernel) == 0 {
		t.Fatal("the kernel carries no classes: the check would pass by reading nothing")
	}
	checked := 0
	for _, sheet := range []*css.Sheet{rules(components.ClassLists()), componentState(), base()} {
		if err := sheet.WalkRules(func(selector string, _ []css.Declaration) error {
			for _, name := range classNames(selector) {
				checked++
				if _, listed := kernel[name]; !listed {
					t.Errorf("%s is addressed by %q, which the kernel emits in its own layer, and is listed by no class vocabulary: a client rule naming it ranks above the kernel's own rule for the element it renders", name, selector)
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if checked < len(kernel) {
		t.Errorf("%d class names were read out of the kernel's own selectors and the vocabulary lists %d: the walk is not reading the sheet it is checking", checked, len(kernel))
	}
}
