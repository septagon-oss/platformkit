package ui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/septagon-oss/platformkit/ui/css"
)

// rawColourRE recognizes literal colour syntax after CSS name normalization.
// Quoted text and URL references are masked before matching.
var rawColourRE = regexp.MustCompile(`#[0-9a-f]{3,8}\b|\b(?:rgb|hsl|oklab|oklch|lab|lch|hwb)a?\(|\bcolor(?:-mix)?\(`)

// boundaryRE refuses braces and comment starts in emitted client text,
// because the stylesheet emitter owns block boundaries.
var boundaryRE = regexp.MustCompile(`[{}]|/\*`)

// styleCloseRE refuses HTML style terminators in emitted CSS, regardless
// of case or CSS quoting, so inline previews cannot escape their element.
var styleCloseRE = regexp.MustCompile(`(?i)</style`)

// customDeclRE recognizes declarations in the kernel custom-property
// namespace after name normalization. Reading a variable remains legal.
var customDeclRE = regexp.MustCompile(`^\s*--pk-`)

// atRuleHead returns a leading at-keyword from a selector-list member,
// skipping quoted and bracketed data. Empty means no member begins an at-rule.
func atRuleHead(selector string) string {
	for at := 0; ; {
		if at = skipSpace(selector, at); at >= len(selector) {
			return ""
		}
		if selector[at] == '@' {
			name, _ := readIdent(selector, at+1) // the name the browser resolves escapes in
			return "@" + name
		}
		for ; at < len(selector) && selector[at] != ','; at++ {
			switch selector[at] {
			case '[':
				at = endOfBracket(selector, at) - 1 // the loop's at++ steps past the ]
			case '"', '\'':
				at = skipQuoted(selector, at) - 1
			}
		}
		at++ // past the comma: the next part of the list begins its own selector
	}
}

// unbraced reports whether text carries c outside a bracketed attribute or a
// quoted value. attrNames and atRuleHead step over those two for the same reason:
// inside one the byte is character data in a token the browser has already
// opened, and outside one it is the syntax a browser acts on — [data-email="a;b"]
// keeps its semicolon and the one after it ends the rule.
func unbraced(text string, c byte) bool {
	for at := 0; at < len(text); at++ {
		switch text[at] {
		case c:
			return true
		case '[':
			at = endOfBracket(text, at) - 1
		case '"', '\'':
			at = skipQuoted(text, at) - 1
		}
	}
	return false
}

// outsideValues masks closed strings and URL arguments with spaces so
// colour checks ignore character data without joining adjacent tokens.
// Open spans stay visible; input is already normalized by resolveNames.
func outsideValues(text string) string {
	out := []byte(text)
	blank := func(from, until int) {
		for i := from; i < until; i++ {
			out[i] = ' '
		}
	}
	for at := 0; at < len(text); {
		if text[at] == '"' || text[at] == '\'' {
			// skipQuoted stops past the quote that closed the string, or at the end of
			// the text; the byte before that offset says which of the two happened.
			if end := skipQuoted(text, at); text[end-1] == text[at] {
				blank(at+1, end)
				at = end
				continue
			}
		} else if strings.HasPrefix(text[at:], "url(") {
			if close := strings.IndexByte(text[at+4:], ')'); close >= 0 {
				blank(at+4, at+4+close)
				at += 4 + close + 1
				continue
			}
		}
		at++
	}
	return string(out)
}

// refuseClientSheet panics when emitted client CSS crosses a stylesheet
// boundary, names a protected hook or class, or writes kernel palette values.
// It checks names rather than selector reachability; see ui/README.md.
func refuseClientSheet(sheet *css.Sheet, classes map[string]string) {
	if sheet.UsesLayers() {
		panic("ui: a client sheet declares its own @layer; Compose places every client rule in the client layer")
	}
	if err := sheet.Verbatim(func(text string) error {
		if close := styleCloseRE.FindString(text); close != "" {
			return fmt.Errorf("ui: a client rule carries %q inside %.120q: the kernel renders the composed sheet as the content of a <style> element, whose text an HTML parser ends at %s, so text carrying it ends the stylesheet the emitter wrote and the bytes after it are read as markup, outside the sheet, outside every layer and in a different language; a stylesheet never needs the sequence, and a value that quotes markup is text the kernel renders, not a sheet's", close, text, close)
		}
		if at := boundaryRE.FindString(text); at != "" {
			return fmt.Errorf("ui: a client rule carries %q inside %.120q: Compose writes what opens and closes a block, so text that carries a brace or a comment start ends the block it sits in and the bytes after it are emitted outside the client layer the gate reads, which is how a consumer's rules leave their layer; a rule that needs a block is a @media or @keyframes, which the emitter closes", at, text)
		}
		return nil
	}); err != nil {
		panic(err.Error())
	}
	if err := sheet.Heads(func(head string) error {
		if at := atRuleHead(head); at != "" {
			return fmt.Errorf("ui: a client rule's head %.120q begins with %s, which is not a selector: Compose writes this text ahead of the `{` it supplies, so an at-keyword there names the block a browser is then reading rather than a rule's subject, and a rule that begins %s states a block of its own inside the client layer Compose places it in; a consumer rule that needs a block writes css.Sheet.Media or css.Sheet.Keyframes, whose braces the emitter closes", head, at, at)
		}
		if unbraced(head, ';') {
			return fmt.Errorf("ui: a client rule's head %.120q carries a `;` ahead of the `{` Compose supplies: a browser ends a rule at `;`, so the text after one is read as the head of the next rule inside the client layer, and a head that says `.store-card; @layer base` or a @media query that says `all; @layer tokens` states an at-rule of its own where the emitter wrote one rule; a `;` inside a bracketed attribute value is data in a token the browser already opened and stays legal", head)
		}
		return nil
	}); err != nil {
		panic(err.Error())
	}
	err := sheet.WalkRules(func(selector string, decls []css.Declaration) error {
		if resolved := resolveNames(selector); strings.Contains(resolved, ":root") {
			return fmt.Errorf("ui: a client rule names %q, which the browser reads as %q: the root element is the kernel's to style, and the tokens it carries are named in one place", selector, resolved)
		}
		for _, match := range attrMatches(selector) {
			// [class] asks whether the element carries the attribute, and every
			// element does: like `*` it names neither vocabulary, and the limit
			// stated below is the one that covers it. Anything that compares the
			// contents names a class — `~=` reads a whole word of the value, the
			// rest read a piece of it — and the gate refuses class names.
			if match.name == classAttr && match.value {
				return fmt.Errorf("ui: a client rule %s compares the contents of the class attribute: that attribute's value is the element's class names, which is the vocabulary this gate refuses a client rule to name, addressed by value rather than by a `.` — [class~=\"sr-only\"] matches exactly the elements `.sr-only` does, and a `.name` selector is the spelling a client owns; the refusal reads names, not what a selector can reach, and it reads every comparison of the value because `*=`, `^=`, `$=` and `|=` reach a class on a partial match; write a class of your own and style it with .name", selector)
			}
		}
		for _, name := range attrNames(selector) {
			if renderedBy, kernel := kernelHooks[name]; kernel {
				return fmt.Errorf("ui: a client rule names the kernel's own attribute %q (%s), which %s renders: the refusal reads attribute names, not what a selector can reach, so a rule beside your own class is refused too; style your own hook and let the layer carry it", name, selector, renderedBy)
			}
		}
		for _, name := range classNames(selector) {
			if renderedBy, kernel := classes[name]; kernel {
				return fmt.Errorf("ui: a client rule names the kernel's own class %q (%s), which %s declares and Compose gives a rule in @layer components: the sheet's own order statement declares that layer before the client layer this rule sits in, so for a normal declaration your rule wins every element the kernel renders that class on, whatever its own selector says — the name this refusal takes, and not the ranking, is what protects the component; the refusal reads class names, not what a selector can reach, so a rule beside your own class is refused too; style a class of your own and let the layer carry it", name, selector, renderedBy)
			}
		}
		for _, d := range decls {
			// The gate reads what the browser will read: the declaration as the
			// emitter writes it, `property: value;`. A semicolon in either field ends
			// a declaration there, so the text after one is a second declaration and
			// whatever precedes its colon is the property a browser will name — the
			// kernel's namespace or a raw colour included. Reading the two fields
			// apart, or one of them, is how the escape survives a check.
			for part := range strings.SplitSeq(d.CSS(), ";") {
				resolved := resolveNames(part)
				if customDeclRE.MatchString(resolved) {
					return fmt.Errorf("ui: a client rule (%s) declares a --pk- property: %.120q is a declaration to a browser, whose property reads as %q — the --pk- namespace is the kernel's to name whatever spelling it is written in, and a semicolon in a property or a value makes the text after it a second declaration", d.Property, part, declaredKey(part))
				}
				if raw := rawColourRE.FindString(outsideValues(resolved)); raw != "" {
					return fmt.Errorf("ui: a client rule (%s) carries the raw colour %q in %.120q; read a token with css.VarRef", d.Property, raw, part)
				}
			}
		}
		return nil
	})
	if err != nil {
		panic(err.Error())
	}
}
