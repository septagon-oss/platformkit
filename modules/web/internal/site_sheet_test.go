package internal

// Review round 13 of T-0108, at the boundary round 12 named as the general form of
// round 11's HIGH: the classes this module hands `ui.Compose` in Extra.Lists
// resolve into @layer components — the kernel's own layer of the sheet every page
// of this application serves — so each of them needs the same refusal a component's
// class has, in both of the two spellings a class is addressable in.
//
// Round 12's file in this directory checks the `.name` spelling, and the cure
// widened the vocabulary from `components.ClassLists()` to the composition's own
// lists. What no pin in the tree does is read the class names off the sheet this
// module actually serves and ask for the second spelling — [class~="name"], which
// the browser resolves to the same element and which the gate read as nothing but
// an attribute name until round 12's cure. A class that reaches @layer components
// by a road other than `Extra.Lists` (a hand-written rule head, a future merge) is
// invisible to a pin that reads `lists()`, and that is the failure mode both
// earlier rounds found. So this reads the emitted sheet.
//
// The premise is the served sheet, which exists today; the assertion is the
// refusal, reached through its own words.

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/css"
)

// reviewRound13SiteSheet is the sheet this module serves: the same call Mount
// makes, with the site's own lists and its prose sheet.
func reviewRound13SiteSheet() string {
	return string(ui.Compose(design.Default(), ui.Extra{Lists: lists(), Sheets: []*css.Sheet{prose()}}).Body)
}

func reviewRound13SiteComponents(t *testing.T, sheet string) string {
	t.Helper()
	open := "@layer components {"
	start := strings.Index(sheet, open)
	if start < 0 {
		t.Fatal("the sheet this site serves carries no @layer components block")
	}
	rest := sheet[start+len(open):]
	depth, out := 1, strings.Builder{}
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return out.String()
			}
		}
		out.WriteByte(rest[i])
	}
	t.Fatal("@layer components is never closed")
	return ""
}

// reviewRound13SiteRefusal composes one consumer rule into the sheet this site
// serves, the way Mount would, and returns the refusal or the accepted length.
func reviewRound13SiteRefusal(t *testing.T, selector string) (refusal string, body int) {
	t.Helper()
	rule := css.NewSheet()
	rule.Select(selector, css.Decl("color", css.VarRef("pk-color-accent-default", "")))
	func() {
		defer func() {
			if r := recover(); r != nil {
				refusal = fmt.Sprint(r)
			}
		}()
		body = len(ui.Compose(design.Default(), ui.Extra{Lists: lists(), Sheets: []*css.Sheet{rule}}).Body)
	}()
	return refusal, body
}

// TestEveryClassTheSiteServesIsRefusedByValue is the second spelling,
// read off the served sheet: every class the site's sheet carries in
// @layer components — the components' utilities, this module's own markup and the
// prose sheet's classes together — must be refused to a consumer rule that matches
// the contents of its class attribute, because that selector styles exactly the
// element this module renders and the client layer outranks the site's own rule.
func TestEveryClassTheSiteServesIsRefusedByValue(t *testing.T) {
	block := reviewRound13SiteComponents(t, reviewRound13SiteSheet())
	seen := map[string]bool{}
	var names []string
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimRight(line, " \t")
		if !strings.HasSuffix(line, "{") {
			continue
		}
		head := strings.TrimSpace(strings.TrimSuffix(line, "{"))
		if head == "" || strings.HasPrefix(head, "@") {
			continue
		}
		for _, name := range reviewRound13SiteHeadClasses(head) {
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	t.Logf("the sheet this site serves carries %d distinct classes in @layer components", len(names))
	if len(names) < 20 {
		t.Fatalf("only %d classes were read off the served sheet: the read has gone blind", len(names))
	}
	for _, name := range names {
		byValue := `[class~="` + name + `"]`
		refusal, body := reviewRound13SiteRefusal(t, byValue)
		if refusal == "" {
			t.Errorf("ui.Compose took a consumer rule at %s: the site's sheet addresses .%s in @layer components and the client layer ranks last, so the consumer's rule wins the site's own element; the sheet carries it at %d bytes", byValue, name, body)
			continue
		}
		if body != 0 {
			t.Errorf("a refused composition (%s) still returned %d bytes of sheet", byValue, body)
		}
	}
}

// reviewRound13SiteHeadClasses returns each class a rule head addresses, spelled as
// the sheet spells it: escapes kept, so a quoted value decodes to the token the
// identifier does; a `.` inside a bracketed value names no class.
func reviewRound13SiteHeadClasses(head string) []string {
	var out []string
	for i := 0; i < len(head); i++ {
		switch head[i] {
		case '[':
			for i < len(head) && head[i] != ']' {
				if head[i] == '\\' {
					i++
				}
				i++
			}
		case '\\':
			i++
		case '.':
			var token strings.Builder
			j := i + 1
			for ; j < len(head); j++ {
				c := head[j]
				if c == '\\' {
					token.WriteByte('\\')
					j++
					if j < len(head) {
						token.WriteByte(head[j])
					}
					continue
				}
				if strings.ContainsRune(".:>+,~*[", rune(c)) {
					break
				}
				token.WriteByte(c)
			}
			if token.Len() > 0 {
				out = append(out, token.String())
			}
			i = j - 1
		}
	}
	return out
}
