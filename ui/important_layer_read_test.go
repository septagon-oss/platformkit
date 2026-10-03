package ui_test

import (
	"os"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/css"
)

// Review round 19's pin: the reduced-motion floor is a *placement*, and a case that
// guards it with a list of property names guards the sheet as it was written.
//
// e2e/reduced-motion-floor.spec.ts measures the served sheet in
// Chromium — that is the right place for the browser fact — and one of its lines is
// `properties.includes(property)` over a list of six property names. The names are
// what this branch happens to emit; they are not the invariant. Any other branch
// that gives a kernel component an `!important` on a property not on the list — a
// `pointer-events` on an overlay, a `will-change` on a sticky shell — would fail a
// case that says nothing about it, which is the tax the programme's own rule on
// pinned numbers refuses. The invariant that survives a new property is where the
// important declaration sits, not which property carries it, and it is a property of
// these bytes alone: this branch is the only writer of the four layers.
//
// So the case below reads the emitted text by position and names no property:
// every `!important` sits inside one of the four `@layer` blocks, and the
// reduced-motion block the floor is made of is inside `@layer base` — earlier layer
// wins an `!important`, which is why base can silence a client's. It then puts a
// consumer `!important` on a property the kernel has never used through the
// composition and asks the same question of it: placed in `@layer client`, never
// unlayered, where an unlayered rule would outrank all four layers at once.
//
// ui.Compose is the subject, not ui.Assets: rounds 9 and 13 already read the served
// bytes, and what is unguarded here is the shape of the emitted text against a
// property list another reviewer's file has to keep up with.

var r19Layers = map[string]bool{"tokens": true, "base": true, "components": true, "client": true}

// r19Scan walks CSS text the way a brace does, and reports each `!important` with the name of the
// `@layer` block a browser would resolve it in ("" outside every block) and the selector of the rule
// that carries it. Strings and comments are skipped: a `content: "}"` is not a block, and a scanner
// that read one would mis-nest everything after it.
func r19Scan(t *testing.T, text string) []r19Important {
	t.Helper()
	var importants []r19Important
	var stack []r19Block // one per open brace: the layer it resolves in, and the head it opens with
	deepest := func() string {
		layer := ""
		for _, b := range stack {
			if b.layer != "" {
				layer = b.layer
			}
		}
		return layer
	}
	for i := 0; i < len(text); {
		switch c := text[i]; {
		case c == '/' && i+1 < len(text) && text[i+1] == '*':
			end := strings.Index(text[i+2:], "*/")
			if end < 0 {
				t.Fatal("the sheet opens a comment it never closes")
			}
			i += end + 4
		case c == '"' || c == '\'':
			i = r19SkipString(t, text, i)
		case c == '{':
			name := ""
			start := i - 1
			for start >= 0 && text[start] != '}' && text[start] != '{' && text[start] != ';' {
				start--
			}
			head := strings.TrimSpace(text[start+1 : i])
			if rest, ok := strings.CutPrefix(head, "@layer "); ok {
				if name = strings.TrimSpace(strings.SplitN(rest, " ", 2)[0]); !r19Layers[name] {
					t.Fatalf("the sheet opens @layer %q, outside the four it declares: %q", name, head)
				}
			}
			stack = append(stack, r19Block{layer: name, head: head})
			i++
		case c == '}':
			if len(stack) == 0 {
				t.Fatal("the sheet closes a block it never opened")
			}
			stack = stack[:len(stack)-1]
			i++
		case strings.HasPrefix(strings.ToLower(text[i:]), "!important"):
			selector := ""
			for j := len(stack) - 1; j >= 0; j-- {
				if !strings.HasPrefix(stack[j].head, "@") {
					selector = stack[j].head
					break
				}
			}
			importants = append(importants, r19Important{
				layer: deepest(), selector: selector, at: i, property: r19PropertyBefore(text, i)})
			i += len("!important")
		default:
			i++
		}
	}
	return importants
}

type r19Block struct{ layer, head string }

type r19Important struct {
	layer    string
	selector string
	at       int
	property string
}

// r19PropertyBefore reads the property name a declaration's value starts from, back to
// the colon that separates them. It is reported, never asserted: a case that asserted
// on it would be the list this case exists to replace.
func r19PropertyBefore(text string, at int) string {
	colon := strings.LastIndexByte(text[:at], ':')
	start := colon
	for start > 0 && (r19IsNameByte(text[start-1]) || text[start-1] == '-') {
		start--
	}
	if colon < 0 || start >= colon {
		return "?"
	}
	return strings.ToLower(strings.TrimSpace(text[start:colon]))
}

func r19IsNameByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

func r19SkipString(t *testing.T, text string, i int) int {
	t.Helper()
	quote := text[i]
	for j := i + 1; j < len(text); j++ {
		switch text[j] {
		case '\\':
			j++
		case quote:
			return j + 1
		}
	}
	t.Fatalf("the sheet opens a string at %d it never closes", i)
	return len(text)
}

func TestEveryImportantDeclarationSitsInsideALayer(t *testing.T) {
	sheet := ui.Compose(design.Default())
	text := string(sheet.Body)
	if v := os.Getenv("PKIT_R19_DUMP"); v != "" {
		os.WriteFile(v, sheet.Body, 0o644)
	}
	importants := r19Scan(t, text)
	if len(importants) == 0 {
		t.Fatal("the sheet authors no !important at all, so the floor this case guards is not in it")
	}
	for _, imp := range importants {
		if imp.layer == "" {
			t.Errorf("an !important on %q sits outside every @layer, where it outranks all four: %s",
				imp.property, snippetAt(text, imp.at))
		}
	}
	// The floor itself, by placement rather than by name: the media block that carries
	// an important animation property is inside @layer base, which is what lets it win
	// an !important written later, in a layer the client owns.
	floored := false
	for _, imp := range importants {
		if imp.layer == "base" && strings.HasPrefix(imp.property, "animation") {
			floored = true
		}
	}
	if !floored {
		t.Error("no animation !important sits in @layer base: the reduced-motion floor is not where the layer order can use it")
	}
}

func TestAConsumerImportantOnAKernelUnusedPropertyStaysInItsLayer(t *testing.T) {
	consumer := css.NewSheet()
	consumer.Select(".r19-caret", css.Decl("caret-color", css.Literal("var(--pk-color-accent-default) !important")),
		css.Decl("pointer-events", css.Literal("none !important")))
	sheet := ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{consumer}})
	text := string(sheet.Body)
	importants := r19Scan(t, text)
	found := 0
	for _, imp := range importants {
		if imp.selector != ".r19-caret" {
			continue
		}
		found++
		if imp.layer != "client" {
			t.Errorf("the consumer's !important on %q landed in layer %q, not in client", imp.property, imp.layer)
		}
	}
	if found != 2 {
		t.Fatalf("the rule the consumer wrote carries %d of its own two important declarations", found)
	}
}

func snippetAt(text string, at int) string {
	lo, hi := at-60, at+20
	if lo < 0 {
		lo = 0
	}
	if hi > len(text) {
		hi = len(text)
	}
	return strings.Join(strings.Fields(text[lo:hi]), " ")
}
