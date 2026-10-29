// Package richtext defines the Markdown subset used by rich-text fields.
// It owns source validation and serialization independently of any module.
package richtext

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

var markdown = goldmark.New(goldmark.WithExtensions(extension.GFM), goldmark.WithParserOptions(parser.WithAutoHeadingID()))

// Document is a parsed source value. Its AST is private and is not mutated by readers.
type Document struct {
	source []byte
	root   ast.Node
}

// Class says whether the author can correct an issue on this request.
type Class string

const (
	Correctable Class = "correctable"
	Immutable   Class = "immutable"
)

// Issue identifies an unsupported construct at a one-based source line, says
// what the author can do about it, and says whether that is this request's
// business at all.
type Issue struct {
	Construct string
	Line      int
	Remedy    string
	Class     Class
}

// Refused contains all detected source issues in document order.
type Refused struct{ Issues []Issue }

func (e *Refused) Error() string {
	if e == nil || len(e.Issues) == 0 {
		return "richtext: refused"
	}
	parts := make([]string, 0, len(e.Issues))
	for _, issue := range e.Issues {
		parts = append(parts, fmt.Sprintf("line %d: %s: %s", issue.Line, issue.Construct, issue.Remedy))
	}
	return "richtext: refused: " + strings.Join(parts, "; ")
}

// Key names the construct in the package's own vocabulary, so an application can
// hold the sentence for it in its own catalogues. It is a slug rather than a
// field because the vocabulary is closed: `Validate` and the fixed-point check in
// `Normalise` raise these and nothing else. An issue the table does not name —
// one whose remedy carries a number, like the length ceiling — has no key and
// stays in the language it was written in, which is the honest half of a
// catalogue that is only partly translated.
func (i Issue) Key() string {
	switch {
	case strings.HasPrefix(i.Construct, "heading level "):
		return "heading-level"
	default:
		return constructKeys[i.Construct]
	}
}

var constructKeys = map[string]string{
	"invalid UTF-8":    "invalid-utf8",
	"raw HTML":         "raw-html",
	"link destination": "link-destination",
	"image source":     "image-source",
	"inline image":     "inline-image",
	"image in table":   "image-in-table",
	"list nesting":     "list-nesting",
	"footnote":         "footnote",
	"definition list":  "definition-list",
	"math":             "math",
	"emoji shortcode":  "emoji-shortcode",
	"missing image":    "missing-image",
	"unstorable text":  "unstorable-text",
}

// Localize returns the issue with its construct and its remedy in the language
// text supplies, falling back to the English where an application has not
// translated. The line and class are untouched: they are the machine-readable
// half of the refusal, and a form marks the control by them.
func (i Issue) Localize(text func(key, fallback string) string) Issue {
	key := i.Key()
	if key == "" || text == nil {
		return i
	}
	i.Construct = text("richtext."+key+".construct", i.Construct)
	i.Remedy = text("richtext."+key+".remedy", i.Remedy)
	return i
}

// Parse checks UTF-8 and builds the CommonMark/GFM syntax tree.
func Parse(source string) (*Document, error) {
	if !utf8.ValidString(source) {
		line := 1
		for i := 0; i < len(source); {
			_, size := utf8.DecodeRuneInString(source[i:])
			if size == 1 && source[i] >= 0x80 {
				break
			}
			if source[i] == '\n' {
				line++
			}
			i += size
		}
		return nil, &Refused{Issues: []Issue{{Construct: "invalid UTF-8", Line: line, Remedy: "Use UTF-8 text.", Class: Correctable}}}
	}
	source = strings.ReplaceAll(strings.ReplaceAll(source, "\r\n", "\n"), "\r", "\n")
	// CommonMark replaces NUL with U+FFFD. Keep the parsed source and the
	// stored source identical so plain text agrees with the rendered document.
	source = strings.ReplaceAll(source, "\x00", "\uFFFD")
	b := []byte(source)
	return &Document{source: b, root: markdown.Parser().Parse(text.NewReader(b))}, nil
}

func (d *Document) line(n ast.Node) int {
	if raw, ok := n.(*ast.RawHTML); ok && raw.Segments != nil && raw.Segments.Len() > 0 {
		at := raw.Segments.At(0).Start
		return 1 + strings.Count(string(d.source[:at]), "\n")
	}
	var firstText func(ast.Node) int
	firstText = func(node ast.Node) int {
		if t, ok := node.(*ast.Text); ok && t.Segment.Start >= 0 && t.Segment.Start <= len(d.source) {
			return t.Segment.Start
		}
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			if at := firstText(child); at >= 0 {
				return at
			}
		}
		return -1
	}
	if at := firstText(n); at >= 0 {
		return 1 + strings.Count(string(d.source[:at]), "\n")
	}
	for p := n; p != nil; p = p.Parent() {
		if p.Type() == ast.TypeBlock && p.Lines() != nil && p.Lines().Len() > 0 {
			start := p.Lines().At(0).Start
			return 1 + strings.Count(string(d.source[:start]), "\n")
		}
	}
	return 1
}

var (
	imageURL    = regexp.MustCompile(`^pk-file:([0-9a-fA-F-]{36})$`)
	setextRule  = regexp.MustCompile(`^\s*[=-]+\s*$`)
	atxStart    = regexp.MustCompile(`^\s*#{1,6}(?:\s|$)`)
	unsupported = []struct {
		re   *regexp.Regexp
		name string
	}{
		{regexp.MustCompile(`\[\^[^]]+\]`), "footnote"},
		{regexp.MustCompile(`^\s*:\s+\S`), "definition list"},
		// These two match the construct rather than its character, because
		// ordinary business prose carries both of its characters: "Tickets cost $5
		// and $8." names two prices and "see note:todo: later" names a label, so a
		// refusal there names a construct the author never wrote, in words they
		// cannot act on. A $ span is math only when it reads as TeX — written with
		// $$ on both sides, or holding at least one of the TeX marks \ ^ _ { } —
		// and a :shortcode: is one only at a word boundary, with neither colon
		// inside a word.
		{regexp.MustCompile(`\$\$[^\s$][^$]*\$\$|\$(?:[\^_{}\\]|[^\s$][^$]*[\^_{}\\])[^$]*\$`), "math"},
		{regexp.MustCompile(`(?:^|[^0-9A-Za-z_:]):[a-z][a-z0-9_+-]+:(?:[^0-9A-Za-z_:]|$)`), "emoji shortcode"},
	}
)

// isPunctuation is CommonMark's ESCAPABLE_CHARACTER: an ASCII punctuation
// character. A backslash before anything else is a literal backslash, which is
// why maskedProse only blanks this set.
func isPunctuation(b byte) bool {
	const marks = "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~"
	return b != '\n' && strings.IndexByte(marks, b) >= 0
}

func validLink(raw string) bool {
	if strings.HasPrefix(raw, "//") || strings.HasPrefix(raw, "/\\") || strings.ContainsAny(raw, "\x00\n\r\t\\") {
		return false
	}
	if strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "#") {
		return true
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return u.Host != "" && u.User == nil
	case "mailto":
		return strings.Contains(u.Opaque, "@")
	case "tel":
		return u.Opaque != ""
	}
	return false
}
