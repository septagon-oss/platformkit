package ui

import (
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/septagon-oss/platformkit/ui/components"
)

// The client half of the stylesheet is one rule and two facts. The rule: a
// consumer styles its own markup and nothing else. The facts: every attribute
// the kernel's markup renders, and the name a selector addresses — which is not
// the bytes it spells it with. Both are here, because the gate that reads them
// is refuseClientSheet.

// renderedHooks are the attributes the kernel renders outside ui/components,
// each attributed to the file that writes it: the page shell (ui/document, with
// ui/page writing the same two on its own document), the delete action
// ui/resource puts in every generated screen, and the gallery hooks ui/export
// writes. ui/components exports its own vocabulary — components.Hooks — because
// that markup lives there; these packages import ui rather than the other way
// round, so their names are declared here, beside the gate that reads them, and
// the attribution is checked rather than asserted: hooks_test.go reads the
// markup each name is attributed to (a name in a comment is not markup), refuses
// a name the kernel renders that the list omits and a name the list carries that
// nothing renders — the same pin components' own test runs on its own list,
// with the attribution standing in for the package directory.
//
// What a module renders is not here. The kernel does not know which modules are
// composed, and a name the product writes is the product's to refuse. An
// attribute is that case: nothing hands the kernel a module's markup. A class is
// the other case — a module hands its classes to Compose in Extra.Lists, which is
// how they get a rule in @layer components — so composedClasses reads those off
// the composition instead of off this list.
var renderedHooks = map[string]string{
	"data-confirm":              "ui/resource/resource.go",
	"data-confirm-label":        "ui/resource/resource.go",
	"data-gallery":              "ui/export/storybook.go",
	"data-gallery-controls":     "ui/export/storybook.go",
	"data-gallery-copy":         "ui/export/storybook.go",
	"data-gallery-kind":         "ui/export/storybook.go",
	"data-gallery-layout":       "ui/export/storybook.go",
	"data-gallery-preview-link": "ui/export/storybook.go",
	"data-gallery-prop":         "ui/export/storybook.go",
	"data-gallery-status":       "ui/export/storybook.go",
	"data-gallery-viewport":     "ui/export/storybook.go",
	"data-gallery-width":        "ui/export/storybook.go",
	"data-principal":            "ui/document/document.go",
	"data-request-notice":       "ui/document/document.go",
	"data-session-error":        "ui/document/document.go",
	"data-signin":               "ui/document/document.go",
	"data-theme":                "ui/document/document.go",
}

// kernelHooks is the vocabulary refuseClientSheet compares a selector against,
// as the canonical name of every attribute the kernel renders mapped to the
// source that renders it. The names are canonical in the sense attrNames
// defines: lowercase, escapes decoded, no whitespace.
var kernelHooks = func() map[string]string {
	names := make(map[string]string, len(components.Hooks)+len(renderedHooks))
	for _, hook := range components.Hooks {
		names[hook] = "ui/components"
	}
	for hook, file := range renderedHooks {
		names[hook] = file
	}
	return names
}()

// kernelClasses is the other half of the vocabulary refuseClientSheet compares a
// selector against: every class name the kernel's own markup carries, as the name
// a browser resolves a class selector to, mapped to the source that renders it.
// Compose widens it per sheet with composedClasses, which is what makes the
// promise hold for the sheet a page loads rather than only for ui/components.
//
// The half attrNames reads is one namespace and this is another, and the gate
// needs both because @layer client ranks last: for normal declarations a later
// layer wins whatever the selector says, so a client rule addressed at a class
// the kernel emits outranks the kernel's own rule for that class — .sr-only {
// position: static } puts the kernel's visually hidden text on screen on every
// page that sheet is served on. The class vocabulary is as much the kernel's
// markup as its attributes are, and ui/components declares both.
//
// The names are computed from components.ClassLists() — the same lists ui/style
// resolves into @layer components and the same lists ui.Gallery deduplicates
// against — and not copied out of a stylesheet, because a component that gains a
// class gains its refusal in the same change and cannot be forgotten here.
// components/hooks_test.go runs the completeness check on the lists themselves.
//
// These names keep the ASCII case they were written with, which is the one
// difference from kernelHooks: an HTML attribute name matches case-insensitively
// and an HTML class name does not (measured in this repository's own Chromium —
// class="flex" is matched by .flex and by .\66 lex, and is not matched by .FLEX),
// so folding here would refuse a consumer's own .FLEX for a class it never
// touches. See classNames.
var kernelClasses = sync.OnceValue(func() map[string]string {
	classes := emitted(components.ClassLists())
	// Optional engines render these parts outside the Go class lists; the
	// shared component sheet styles them in the same protected layer.
	out := map[string]string{
		"leaflet-bar":       "ui/assets/js/leaflet-1.9.4.min.js",
		"pk-calendar-event": "ui/assets/js/specialists.js",
		"pk-map-marker":     "ui/assets/js/specialists.js",
		"pk-map-pin":        "ui/assets/js/specialists.js",
	}
	for _, name := range classes {
		out[name] = "ui/components"
	}
	return out
})

// classNames lists every class name a selector addresses, escapes decoded and
// ASCII case kept: `.card`, `.\66 lex` and `.text-fg-brand:hover` each address
// one class, and `.FLEX` addresses a class other than .flex.
//
// The read is attrNames' shape over the other sigil, with the fold dropped for
// the reason in kernelClasses: a `.` inside a bracketed attribute value or a
// quoted string is character data in a token the browser already opened —
// [data-email="a.b"] addresses no class — so the scan steps over both, and an
// escaped `.` is a class name's own character and not a class selector at all.
// A class name inside a functional pseudo-class is named by the selector even
// though the rule matches what the pseudo-class keeps: refuseClientSheet reads
// names and not reachability, and says so.
func classNames(selector string) []string {
	var out []string
	for at := 0; at < len(selector); {
		switch selector[at] {
		case '[':
			at = endOfBracket(selector, at)
		case '"', '\'':
			at = skipQuoted(selector, at)
		case '\\':
			_, past := scanEscape(selector, at) // \2e makes an ident's character, never a class
			at = past
		case '.':
			name, past := scanIdent(selector, at+1)
			if name != "" {
				out = append(out, name)
			}
			at = past
		default:
			at++
		}
	}
	return out
}

// attrNames lists every attribute name a selector addresses, in the one form the
// browser resolves them to: whitespace inside the brackets and around the
// operator dropped, \NN and \X escapes decoded, ASCII case folded (an HTML
// attribute name matches case-insensitively) and the local name taken out of a
// namespace prefix.
//
// The gate cannot compare the selector as written. CSS spells one name several
// ways — `[ data-component=button ]`, `[DATA-COMPONENT=button]`,
// `[\64 ata-component=button]` all address [data-component] — and every spelling
// the reader did not parse is a client rule that reaches the client layer, which
// ranks last, and restyles the kernel's own component.
//
// attrNames is attrMatches projected onto the name: the hook refusal wants names
// and nothing else, and the fact that projection drops — whether a comparison of
// the value follows the name — is the one classAttr needs.
func attrNames(selector string) []string {
	matches := attrMatches(selector)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		out = append(out, match.name)
	}
	return out
}

// attrMatch is one bracketed attribute selector: the name a browser resolves the
// attribute to, in the one form attrNames describes, and whether the selector
// compares that attribute's value ([name~="v"]) or only asks whether the element
// carries the attribute at all ([name]).
type attrMatch struct {
	name  string
	value bool
}

// attrMatches is the read attrNames applies to the name, kept whole so a caller
// can ask the one question attrNames drops: is the selector comparing what is in
// the attribute, or only its presence. That question decides the classAttr
// refusal, whose attribute's value is a namespace of names rather than a token,
// and attrNames is this list stripped of that fact — so the name the hook refusal
// refuses and the name the class refusal looks beside are read by one scan and
// cannot drift apart.
func attrMatches(selector string) []attrMatch {
	var out []attrMatch
	for at := 0; at < len(selector); {
		if selector[at] != '[' {
			at++
			continue
		}
		at = skipSpace(selector, at+1)
		// [ns|name] and [*|name] match on the local name. |= is the operator in
		// [name|=value], not a namespace separator, and * is not an identifier,
		// so the wildcard namespace is read as what stops before it.
		for {
			_, after := readIdent(selector, at)
			if after == at && at < len(selector) && selector[at] == '*' {
				after++
			}
			if after >= len(selector) || selector[after] != '|' ||
				(after+1 < len(selector) && selector[after+1] == '=') {
				break
			}
			at = skipSpace(selector, after+1)
		}
		name, after := readIdent(selector, at)
		if name != "" {
			out = append(out, attrMatch{name: name, value: comparesValue(selector, after)})
		}
		at = endOfBracket(selector, after)
	}
	return out
}

// comparesValue reports whether the attribute whose name ends at at is followed
// by an operator that compares its value: `=` alone, or one of `~`, `*`, `^`,
// `$`, `|` immediately followed by `=`. Whitespace separates a name from its
// operator and nothing inside an operator — `~=` is one token, so `[a ~ = "v"]`
// is no operator to a browser and reads here as no comparison either.
func comparesValue(s string, at int) bool {
	at = skipSpace(s, at)
	if at >= len(s) {
		return false
	}
	if s[at] == '=' {
		return true
	}
	if at+1 >= len(s) || s[at+1] != '=' {
		return false
	}
	switch s[at] {
	case '~', '*', '^', '$', '|':
		return true
	}
	return false
}

// classAttr is the one attribute whose value is not a token but a list of names
// the kernel owns: an element's class names. It is therefore the second
// vocabulary refuseClientSheet reads, addressed by value instead of by a `.` —
// [class~="sr-only"] matches exactly the elements `.sr-only` matches, and
// attrNames alone reads it as an attribute no kernel markup is addressed by,
// because `class` is not a hook and cannot be one: every element carries it.
// Matching the contents of class= is the class namespace by the back door, which
// is why the gate refuses the comparison; asking only whether the element carries
// the attribute is not, because every element does and the read names nothing —
// see attrMatch.value and the limit refuseClientSheet states.
const classAttr = "class"

// scanIdent decodes a CSS identifier (Syntax §4.2) at the offset and keeps its
// case: letters, digits, -, _, any byte above ASCII, and \ followed by a
// character or by up to six hex digits whose terminating whitespace is part of
// the escape. It returns the decoded name and the offset just past it, or the
// empty name when no identifier starts there.
func scanIdent(s string, at int) (string, int) {
	var name []byte
	for at < len(s) {
		switch c := s[at]; {
		case c == '\\':
			decoded, past := scanEscape(s, at)
			if decoded == nil {
				return string(name), past // a `\` that stands for nothing ends the name
			}
			name = append(name, decoded...)
			at = past
		case c == '-' || c == '_' || isASCIILetter(c) || isDigit(c) || c >= utf8.RuneSelf:
			name = append(name, c)
			at++
		default:
			return string(name), at
		}
	}
	return string(name), at
}

// readIdent is the identifier a case-insensitive name: an attribute name, an
// at-keyword, a keyword. It keeps its escapes and drops its ASCII case, because
// those are the names an HTML document matches without regard to how they were
// written. classNames wants the same scan and not the fold.
func readIdent(s string, at int) (string, int) {
	name, past := scanIdent(s, at)
	return foldASCII(name), past
}

// foldASCII lower-cases the ASCII letters in text and leaves every other byte
// alone, which is the whole of what CSS calls ASCII case-insensitivity for a
// name, applied once to a decoded name instead of byte by byte while it is read.
func foldASCII(s string) string {
	out := []byte(s)
	for i, c := range out {
		out[i] = lowerASCII(c)
	}
	return string(out)
}

// readEscape is scanEscape with the ASCII case folded, for the reads that
// compare case-insensitive names — resolveNames, which decides what :root, a
// --pk- property and a colour function are spelled like.
func readEscape(s string, at int) ([]byte, int) {
	decoded, past := scanEscape(s, at)
	if decoded == nil {
		return nil, past
	}
	return []byte(foldASCII(string(decoded))), past
}

// scanEscape decodes the escape sequence whose `\` stands at at, and returns the
// bytes the browser reads that character as together with the offset just past
// the whole sequence. Two spellings reach the same character: \c for a character
// c that is not a hex digit, and \ followed by up to six hex digits, at most
// 0xffffff, so there is no value this read can fail on. The single whitespace
// character that terminates a hex escape is part of the escape and not of the
// text, so it is consumed here and answers no later question — which is what
// makes `:ro\6f t` one name and `--\70 k-color-accent-default` one property.
// A `\` with nothing after it stands for no character and returns nil bytes.
//
// The case the escape spells is the author's: `\46 lex` is FLEX and `\66 lex` is
// flex, which is the difference between a class the kernel emits and one it does
// not, so the fold is a caller's decision and not this one's.
func scanEscape(s string, at int) ([]byte, int) {
	at++
	if at >= len(s) {
		return nil, at
	}
	if !isHexDigit(s[at]) {
		return []byte{s[at]}, at + 1
	}
	value := int32(0)
	for start := at; at < len(s) && at-start < 6 && isHexDigit(s[at]); at++ {
		value = value*16 + hexValue(s[at])
	}
	out := utf8.AppendRune(nil, rune(value))
	if at < len(s) && isCSSSpace(s[at]) {
		at++
	}
	return out, at
}

// hexValue is the value a hexadecimal digit spells.
func hexValue(c byte) int32 {
	switch {
	case isDigit(c):
		return int32(c - '0')
	case 'a' <= c && c <= 'f':
		return int32(c-'a') + 10
	default:
		return int32(c-'A') + 10
	}
}

// resolveNames spells a fragment of a sheet the way a browser resolves it: every
// escape decoded to the character it stands for and every ASCII letter folded to
// lower case, which is readIdent's read widened from one attribute name to the
// whole text. It is what the gate compares against, because these are names and
// not bytes — a pseudo-class is a keyword matched case-insensitively, a custom
// property's name decodes escapes whatever its author's casing, and a colour
// function's name and a hash colour's digits are read the same way. The read is
// deliberately wider than a tokeniser, in attrNames' chosen direction: it decodes
// an escape wherever it stands, so a fragment that spells a kernel name through an
// escape no token of that kind would carry is refused beside the spellings that
// are real. Refusing too wide costs a consumer one renamed rule; reading too
// narrow is a client rule that reaches the client layer, which ranks last.
func resolveNames(text string) string {
	out := make([]byte, 0, len(text))
	for at := 0; at < len(text); {
		if text[at] != '\\' {
			out = append(out, lowerASCII(text[at]))
			at++
			continue
		}
		decoded, past := readEscape(text, at)
		if decoded == nil {
			out = append(out, '\\')
			at = past
			continue
		}
		out = append(out, decoded...)
		at = past
	}
	return string(out)
}

// declaredKey is the property a browser reads out of one declaration part — the
// text before its first colon, resolved the way resolveNames resolves a name — so
// a refusal names the property it refused rather than the bytes that spelled it.
func declaredKey(part string) string {
	name, _, _ := strings.Cut(resolveNames(part), ":")
	return strings.TrimSpace(name)
}

// endOfBracket returns the offset just past the ] that closes the bracket opened
// before at, so a quoted value that contains ] is not read as more selector.
func endOfBracket(s string, at int) int {
	for at < len(s) {
		switch s[at] {
		case ']':
			return at + 1
		case '"', '\'':
			at = skipQuoted(s, at)
		case '\\':
			at += 2
		default:
			at++
		}
	}
	return at
}

func skipQuoted(s string, at int) int {
	quote := s[at]
	for at++; at < len(s); {
		switch s[at] {
		case '\\':
			at += 2
		case quote:
			return at + 1
		default:
			at++
		}
	}
	return at
}

func skipSpace(s string, at int) int {
	for at < len(s) && isCSSSpace(s[at]) {
		at++
	}
	return at
}

func isCSSSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

func isHexDigit(c byte) bool {
	return isDigit(c) || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F')
}

func isDigit(c byte) bool { return '0' <= c && c <= '9' }

func isASCIILetter(c byte) bool {
	return ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}

func lowerASCII(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}
