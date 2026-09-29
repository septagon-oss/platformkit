package ui

import (
	"strconv"
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
// composed, and a name the product writes is the product's to refuse.
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
func attrNames(selector string) []string {
	var out []string
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
			out = append(out, name)
		}
		at = endOfBracket(selector, after)
	}
	return out
}

// readIdent decodes a CSS identifier (Syntax §4.2) at the offset: letters,
// digits, -, _, any byte above ASCII, and \ followed by a character or by up to
// six hex digits whose terminating whitespace is part of the escape. It returns
// the decoded name with ASCII folded to lower case and the offset just past it,
// or the empty name when no identifier starts there.
func readIdent(s string, at int) (string, int) {
	var name []byte
	stopped := at
	for at < len(s) {
		switch c := s[at]; {
		case c == '\\':
			at++
			if at >= len(s) {
				return string(name), at
			}
			if !isHexDigit(s[at]) {
				name = append(name, lowerASCII(s[at]))
				stopped, at = at+1, at+1
				continue
			}
			start := at
			for at < len(s) && at-start < 6 && isHexDigit(s[at]) {
				at++
			}
			runeValue, err := strconv.ParseInt(s[start:at], 16, 32)
			if err != nil {
				return string(name), stopped
			}
			r := rune(runeValue)
			if 'A' <= r && r <= 'Z' {
				r += 'a' - 'A'
			}
			name = utf8.AppendRune(name, r)
			stopped = at
			if at < len(s) && isCSSSpace(s[at]) {
				at++
			}
		case c == '-' || c == '_' || isASCIILetter(c) || isDigit(c) || c >= utf8.RuneSelf:
			name = append(name, lowerASCII(c))
			stopped, at = at+1, at+1
		default:
			return string(name), stopped
		}
	}
	return string(name), stopped
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
