// Package richtext defines the Markdown subset used by rich-text fields.
// It owns source validation and serialization independently of any module.
package richtext

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
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
// field because the vocabulary is closed: Validate raises these and nothing
// else. An issue the table does not name — one whose remedy carries a number,
// like the length ceiling — has no key and stays in the language it was written
// in, which is the honest half of a catalogue that is only partly translated.
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
		{regexp.MustCompile(`\$[^\s$][^$]*\$`), "math"},
		{regexp.MustCompile(`:[a-z][a-z0-9_+-]+:`), "emoji shortcode"},
	}
)

// isPunctuation is CommonMark's ESCAPABLE_CHARACTER: an ASCII punctuation
// character. A backslash before anything else is a literal backslash, which is
// why the escape pass below only blanks this set.
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

// Validate returns every disallowed construct without changing the document.
func Validate(d *Document) []Issue {
	if d == nil {
		return []Issue{{Construct: "document", Line: 1, Remedy: "Supply a document.", Class: Immutable}}
	}
	var issues []Issue
	add := func(n ast.Node, name, remedy string) {
		issues = append(issues, Issue{Construct: name, Line: d.line(n), Remedy: remedy, Class: Correctable})
	}
	_ = ast.Walk(d.root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := n.(type) {
		case *ast.Heading:
			if v.Level < 2 || v.Level > 4 {
				add(n, fmt.Sprintf("heading level %d", v.Level), "Use headings ## through ####; the page title is the top heading.")
			}
		case *ast.RawHTML, *ast.HTMLBlock:
			add(n, "raw HTML", "Write Markdown instead of HTML.")
		case *ast.Link:
			if !validLink(string(v.Destination)) {
				add(n, "link destination", "Use https:, http:, mailto:, tel:, a site path or an anchor.")
			}
		case *ast.AutoLink:
			if !validLink(string(v.URL(d.source))) {
				add(n, "link destination", "Use an allowed link destination.")
			}
		case *ast.Image:
			match := imageURL.FindStringSubmatch(string(v.Destination))
			if len(match) != 2 {
				add(n, "image source", "Upload the image so it is stored with this site.")
			} else if _, err := uuid.Parse(match[1]); err != nil {
				add(n, "image source", "Upload the image so it is stored with this site.")
			}
			if n.Parent() == nil || n.Parent().Kind() != ast.KindParagraph || n.PreviousSibling() != nil || n.NextSibling() != nil {
				add(n, "inline image", "Put the image alone on its own line.")
			}
			for p := n.Parent(); p != nil; p = p.Parent() {
				if _, ok := p.(*extast.TableCell); ok {
					add(n, "image in table", "Put the image outside the table.")
					break
				}
			}
		case *ast.List:
			depth := 0
			for p := n; p != nil; p = p.Parent() {
				if _, ok := p.(*ast.List); ok {
					depth++
				}
			}
			if depth > 3 {
				add(n, "list nesting", "Use at most three list levels.")
			}
		}
		return ast.WalkContinue, nil
	})
	// Regex-only checks cover syntax Goldmark treats as ordinary text. Mask
	// literal code first: neither a fenced block nor an inline code span can
	// introduce an unsupported Markdown construct.
	prose := append([]byte(nil), d.source...)
	mask := func(start, stop int) {
		for i := start; i < stop; i++ {
			if prose[i] != '\n' {
				prose[i] = ' '
			}
		}
	}
	_ = ast.Walk(d.root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n.(type) {
		case *ast.CodeBlock, *ast.FencedCodeBlock:
			for i := 0; i < n.Lines().Len(); i++ {
				segment := n.Lines().At(i)
				mask(segment.Start, segment.Stop)
			}
		case *ast.CodeSpan:
			for child := n.FirstChild(); child != nil; child = child.NextSibling() {
				segment := child.(*ast.Text).Segment
				mask(segment.Start, segment.Stop)
			}
		}
		return ast.WalkContinue, nil
	})
	// A backslash before ASCII punctuation is CommonMark's escape: the character
	// after it is data, so `\:smile:` is a smiley nobody asked for and `\[^x]`
	// a footnote nobody wrote. Blank the pair the same way literal code above
	// is blanked, so the line keeps its length and its column count. `\\:smile:`
	// stays a shortcode, because there the first backslash escapes the second.
	for i := 0; i < len(prose)-1; i++ {
		if prose[i] == '\\' && isPunctuation(prose[i+1]) {
			prose[i], prose[i+1] = ' ', ' '
			i++
		}
	}
	for i, line := range strings.Split(string(prose), "\n") {
		for _, rule := range unsupported {
			if rule.re.MatchString(line) {
				issues = append(issues, Issue{
					Construct: rule.name, Line: i + 1,
					Remedy: "Use the supported Markdown constructs.", Class: Correctable,
				})
			}
		}
	}
	sort.SliceStable(issues, func(i, j int) bool { return issues[i].Line < issues[j].Line })
	return issues
}

// Normalise validates a value and returns LF-terminated canonical Markdown.
func Normalise(source string) (string, error) {
	d, err := Parse(source)
	if err != nil {
		return "", err
	}
	if issues := Validate(d); len(issues) > 0 {
		return "", &Refused{Issues: issues}
	}
	// A text node's hard-break flag is the parser's decision that trailing
	// spaces are syntax. Keep exactly two; trimming them would silently turn
	// an allowed hard break into a soft break on the next parse.
	hardBreak := map[int]bool{}
	setext := map[int]int{}
	var newlines []int
	for i, b := range d.source {
		if b == '\n' {
			newlines = append(newlines, i)
		}
	}
	lineAt := func(offset int) int { return sort.SearchInts(newlines, offset) }
	_ = ast.Walk(d.root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if v, ok := n.(*ast.Text); ok && v.HardLineBreak() {
			line := lineAt(v.Segment.Stop)
			hardBreak[line] = true
		}
		if v, ok := n.(*ast.Heading); ok && n.Parent() == d.root && n.Lines().Len() == 1 {
			line := lineAt(n.Lines().At(0).Start)
			setext[line] = v.Level
		}
		return ast.WalkContinue, nil
	})
	// The lines of a code block are data. Trailing spaces inside a fence are
	// part of the value the author stored, and a blank line inside one is part
	// of it too, so neither the trimming, nor the blank-run compaction, nor the
	// heading rewrite below may touch them.
	protected := map[int]bool{}
	bullets := map[int]bool{}
	_ = ast.Walk(d.root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := n.(type) {
		case *ast.List:
			// `*`, `+` and `-` open the same list. Store one spelling of it, so
			// two documents with one syntax tree have one hash. Ordered markers
			// keep their number: that number is the list's start, not a style.
			if v.IsOrdered() || v.Marker == '-' {
				return ast.WalkContinue, nil
			}
			for item := v.FirstChild(); item != nil; item = item.NextSibling() {
				// The marker itself is not in the tree: the item's first block
				// starts just after it, on the marker's own line.
				if child := item.FirstChild(); child != nil && child.Lines().Len() > 0 {
					bullets[lineAt(child.Lines().At(0).Start)] = true
				}
			}
		case *ast.CodeBlock, *ast.FencedCodeBlock:
			for i := 0; i < n.Lines().Len(); i++ {
				segment := n.Lines().At(i)
				last := segment.Start
				if segment.Stop > segment.Start {
					last = segment.Stop - 1
				}
				for line := lineAt(segment.Start); line <= lineAt(last); line++ {
					protected[line] = true
				}
			}
		}
		return ast.WalkContinue, nil
	})

	type stored struct {
		text    string
		literal bool // copied out of a code block, unchanged
	}
	var out []stored
	lines := strings.Split(string(d.source), "\n")
	for i := 0; i < len(lines); i++ {
		line, literal := lines[i], protected[i]
		if !literal {
			line = strings.TrimRight(line, " \t")
			if hardBreak[i] && strings.HasSuffix(lines[i], "  ") {
				line += "  "
			}
			if bullets[i] {
				lead := strings.TrimLeft(line, " \t")
				if len(lead) > 0 && strings.IndexByte("*+-", lead[0]) >= 0 {
					line = line[:len(line)-len(lead)] + "-" + lead[1:]
				}
			}
			// A Setext heading and its ATX spelling are the same parsed heading.
			// Store one spelling, leaving the following block adjacent as before.
			if level := setext[i]; level >= 2 && level <= 4 && i+1 < len(lines) &&
				!atxStart.MatchString(lines[i]) && setextRule.MatchString(lines[i+1]) {
				line = strings.Repeat("#", level) + " " + strings.TrimSpace(line)
				i++
			}
		}
		out = append(out, stored{line, literal})
	}
	for len(out) > 0 && out[0].text == "" && !out[0].literal {
		out = out[1:]
	}
	for n := len(out) - 1; n >= 0 && out[n].text == "" && !out[n].literal; n-- {
		out = out[:n]
	}
	if len(out) == 0 {
		return "", nil
	}
	// Indented code is accepted input but stored as fenced code.
	for i := 0; i < len(out); i++ {
		if !strings.HasPrefix(out[i].text, "    ") || (i > 0 && out[i-1].text != "") {
			continue
		}
		end := i
		for end < len(out) && strings.HasPrefix(out[end].text, "    ") {
			end++
		}
		code := []stored{{text: "```"}}
		for _, line := range out[i:end] {
			code = append(code, stored{strings.TrimPrefix(line.text, "    "), true})
		}
		code = append(code, stored{text: "```"})
		out = append(append(out[:i:i], code...), out[end:]...)
		i += len(code) - 1
	}
	// Empty runs are one block separator. Preserving line indentation retains
	// list and table structure, and a line of literal code keeps the blanks it
	// was written with, which is what makes a second pass byte-identical.
	compact := make([]stored, 0, len(out))
	for _, line := range out {
		if last := len(compact) - 1; line.text != "" || len(compact) == 0 ||
			compact[last].text != "" || compact[last].literal {
			compact = append(compact, line)
		}
	}
	text := make([]string, len(compact))
	for i, line := range compact {
		text[i] = line.text
	}
	return strings.Join(text, "\n") + "\n", nil
}

// SourceHash is SHA-256 of canonical UTF-8 Markdown, encoded in lowercase hex.
func SourceHash(source string) (string, error) {
	normal, err := Normalise(source)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(normal))
	return hex.EncodeToString(sum[:]), nil
}
