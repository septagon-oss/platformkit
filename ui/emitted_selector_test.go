// Every case in this repository reads a *name*: some feed the gate a
// selector and ask whether refuseClientSheet refuses it, and cascade-layers.spec.ts
// reads the class names Chromium resolved out of the served sheet. Neither half
// alone carries the brief's invariant. The gate reads the text the developer
// handed Compose; the browser acts on the text the emitter wrote out. Those are
// the same bytes only because Rule.CSS writes the selector verbatim — and if they
// ever parted (an emitter that "helpfully" escaped `:`, `[` and `/` in a selector
// being the obvious change), a consumer sheet at `.hover:border-border-secondary`
// would arrive at the browser as the kernel's own escaped class, land in
// @layer client, and win the element ui/components renders with it, because the
// refusal read a name the developer typed and not a name the sheet now carries.
//
// So this file reads the ARTIFACT, the way the browser reads it: for every
// consumer spelling it feeds Compose, either the composition refuses it, or the
// emitted rule's own selector — escapes decoded, brackets and strings stepped
// over, which is how a tokenizer reads an ident — names no class that
// @layer components styles in either served sheet. It says nothing about type
// selectors and ids: ui/ui.go states that the gate reads names and not reach, and
// that a product decides who may restyle a dismissed dialog; this case asserts
// only the one thing the brief's *Done when* claims.
package ui_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/css"
)

// servedRuleHeads returns every rule head in the named @layer block of sheet, read
// off the bytes the emitter wrote. The emitter indents a rule's head onto its own
// line and closes it with `{`; an at-rule prelude line starts with `@` and carries
// no selector, so it is skipped and the heads inside it are still read.
func servedRuleHeads(t *testing.T, sheet, layer string) []string {
	t.Helper()
	open := "@layer " + layer + " {"
	start := strings.Index(sheet, open)
	if start < 0 {
		t.Fatalf("the served sheet carries no @layer %s block:\n%.200s", layer, sheet)
	}
	rest := sheet[start+len(open):]
	var heads []string
	for _, line := range strings.Split(rest, "\n") {
		trimmed := strings.TrimRight(line, " ")
		if !strings.HasSuffix(trimmed, "{") || strings.Contains(trimmed, ";") {
			continue
		}
		head := strings.TrimSuffix(strings.TrimSpace(trimmed), "{")
		if head == "" || strings.HasPrefix(head, "@") {
			continue
		}
		heads = append(heads, head)
	}
	if len(heads) == 0 {
		t.Fatalf("@layer %s of the served sheet carries no rule head", layer)
	}
	return heads
}

// classTokens reads the class names a selector carries the way a CSS tokenizer
// does: an escape is data inside the identifier that holds it (`\:` continues the
// name, a bare `:` ends it and starts a pseudo-class), a bracketed attribute or a
// quoted value is character data a `.` inside it names nothing for, and the escape
// is then decoded, because the name the element carries is the decoded one.
func classTokens(t *testing.T, selector string) []string {
	t.Helper()
	var out []string
	for i := 0; i < len(selector); i++ {
		switch selector[i] {
		case '[':
			for i < len(selector) && selector[i] != ']' {
				if selector[i] == '\\' && i+1 < len(selector) {
					i++
				}
				i++
			}
		case '"', '\'':
			quote := selector[i]
			i++
			for i < len(selector) && selector[i] != quote {
				if selector[i] == '\\' && i+1 < len(selector) {
					i++
				}
				i++
			}
		case '.':
			var name strings.Builder
			for j := i + 1; j < len(selector); {
				if selector[j] == '\\' && j+1 < len(selector) {
					decoded, next := decodeEscape(selector, j)
					name.WriteString(decoded)
					j = next
					continue
				}
				if !isIdentByte(selector[j]) {
					break
				}
				name.WriteByte(selector[j])
				j++
			}
			if name.Len() > 0 {
				out = append(out, name.String())
			}
			i = lastClassTokenEnd(selector, i)
		}
	}
	return out
}

// lastClassTokenEnd returns the offset just past the identifier that begins at the
// `.` at start, so the scan does not re-read a `.` that belongs to the name.
func lastClassTokenEnd(selector string, start int) int {
	for j := start + 1; j < len(selector); {
		if selector[j] == '\\' && j+1 < len(selector) {
			_, j = decodeEscape(selector, j)
			continue
		}
		if !isIdentByte(selector[j]) {
			return j - 1 // the loop's i++ steps onto the byte that ended the name
		}
		j++
	}
	return len(selector)
}

// decodeEscape decodes the escape that begins at the backslash at at, and returns
// the offset past it. A hex escape is one to six digits closed by whitespace or by
// the name; any other escape stands for the character itself.
func decodeEscape(s string, at int) (string, int) {
	j := at + 1
	if j >= len(s) {
		return `\`, j
	}
	if !isHexByte(s[j]) {
		return string(s[j]), j + 1
	}
	digits := 0
	for j < len(s) && isHexByte(s[j]) && digits < 6 {
		j++
		digits++
	}
	if j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n') {
		j++
	}
	value := 0
	for _, c := range s[at+1 : j] {
		value = value*16 + hexValue(byte(c))
	}
	return string(rune(value)), j
}

func isHexByte(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func hexValue(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	default:
		return int(c-'A') + 10
	}
}

func isIdentByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		c == '-' || c == '_' || c >= 0x80
}

// componentsStyledClasses is every class @layer components styles in the two sheets
// a shell links, read out of those sheets' own bytes: app.css carries the kernel's
// rules and the composition's resolved class lists, gallery.css the classes only the
// gallery emits. Both are served by ui.Assets beside the client layer, and the sheet
// that states the layer order states it for the whole document, so a rule in
// @layer client outranks a rule in either.
func componentsStyledClasses(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, sheet := range []string{
		string(ui.Compose(design.Default()).Body),
		string(ui.Gallery().Body),
	} {
		for _, head := range servedRuleHeads(t, sheet, "components") {
			for _, name := range classTokens(t, head) {
				out[name] = true
			}
		}
	}
	if len(out) < 100 {
		t.Fatalf("only %d class names read out of @layer components: the read has gone blind", len(out))
	}
	return out
}

// composeClientBlock returns the @layer client block Compose emitted for a
// consumer rule, or "" when the composition refused it.
func composeClientBlock(t *testing.T, selector string) (block string, refused bool) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			block, refused = "", true
		}
	}()
	sheet := ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{
		css.NewSheet().Select(selector, css.Decl("padding", css.Literal("7px"))),
	}})
	body := string(sheet.Body)
	start := strings.Index(body, "@layer client {")
	if start < 0 {
		t.Fatalf("the composed sheet carries no @layer client block")
	}
	return body[start:], false
}

// TestNoRuleTheGateAcceptsNamesAClassTheComponentsLayerStyles is the
// brief's *Done when* — "a test proves a client rule cannot beat a component rule"
// — read off the emitted sheet rather than off the selector the developer wrote.
func TestNoRuleTheGateAcceptsNamesAClassTheComponentsLayerStyles(t *testing.T) {
	t.Parallel()
	styled := componentsStyledClasses(t)
	// The spellings a consumer has: the kernel's class as the sheet spells it, the
	// same class with a pseudo-class beside it, an escaped spelling of its name, the
	// class addressed by value, and the spellings that carry the kernel's own
	// variant and arbitrary-value names as raw text — the ones a browser reads as a
	// short class plus a pseudo-class or an attribute, so that Compose may take them
	// only because they name nothing. `.store-hero` and `a` are what a client owns:
	// a name the kernel declares no rule for, and a tag, which the gate's stated
	// scope leaves to the product.
	for _, selector := range []string{
		".flex", ".flex:hover", `.\66 lex`, `.hover\:border-border-secondary`,
		`.bg-surface-overlay\/50`, `.animate-pulse`, `[class~="flex"]`,
		`[data-state="featured"].animate-pulse`,
		".hover:border-border-secondary", ".sm:items-center", ".z-[1400]", ".max-h-[85vh]",
		".store-hero", "a",
	} {
		block, refused := composeClientBlock(t, selector)
		if refused {
			continue
		}
		for _, head := range servedRuleHeads(t, block, "client") {
			for _, name := range classTokens(t, head) {
				if styled[name] {
					t.Errorf("ui.Compose accepted a consumer rule and emitted %q, whose selector names the class %q, decoded from the bytes the sheet carries: @layer components of a sheet this shell links styles that class, @layer client ranks after it, so the consumer's rule wins the kernel's own element whatever its selector says, and the refusal read the name the developer typed rather than the name the sheet now carries", head, name)
				}
			}
		}
	}
}

// TestTheReadNamesAKernelClassFromTheSheetsOwnEscapedSpelling is the
// floor under the case above: the tokenizer and the escape decode are pointed at
// the kernel's own emitted spelling of a class that only gallery.css styles, and
// must name it. Without this the case above would be green because it resolved
// nothing, which is the shape of every pin that ever passed by going blind.
func TestTheReadNamesAKernelClassFromTheSheetsOwnEscapedSpelling(t *testing.T) {
	t.Parallel()
	styled := componentsStyledClasses(t)
	gallery := string(ui.Gallery().Body)
	var escaped string
	for _, head := range servedRuleHeads(t, gallery, "components") {
		if strings.Contains(head, `\`) {
			escaped = head
			break
		}
	}
	if escaped == "" {
		t.Fatal("gallery.css spells no class with an escape: the read has nothing to decode")
	}
	resolved := classTokens(t, escaped)
	if len(resolved) == 0 {
		t.Fatalf("the read resolves no class from the kernel's own head %q", escaped)
	}
	named := false
	for _, name := range resolved {
		if styled[name] {
			named = true
		}
		if !strings.Contains(name, `\`) {
			continue
		}
		t.Errorf("the read left an escape undecoded in %q from %q: a browser gives the element the decoded name", name, escaped)
	}
	if !named {
		t.Errorf("the read resolves %q from %q, none of which @layer components styles: the case above would be green because it sees nothing", resolved, escaped)
	}
}
