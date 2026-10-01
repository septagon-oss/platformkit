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

// Issue identifies an unsupported construct at a one-based source line.
type Issue struct {
	Construct string
	Line      int
	Message   string
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
		return nil, &Refused{Issues: []Issue{{"invalid UTF-8", line, "Invalid text encoding", "Use UTF-8 text.", Correctable}}}
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
		return []Issue{{"document", 1, "No document", "Supply a document.", Immutable}}
	}
	var issues []Issue
	add := func(n ast.Node, name, remedy string) {
		issues = append(issues, Issue{name, d.line(n), "Unsupported " + name, remedy, Correctable})
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
	for i, line := range strings.Split(string(d.source), "\n") {
		for _, rule := range unsupported {
			if rule.re.MatchString(line) {
				issues = append(issues, Issue{rule.name, i + 1, "Unsupported " + rule.name, "Use the supported Markdown constructs.", Correctable})
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
	var out []string
	for _, line := range strings.Split(string(d.source), "\n") {
		out = append(out, strings.TrimRight(line, " \t"))
	}
	for len(out) > 0 && out[0] == "" {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	if len(out) == 0 {
		return "", nil
	}
	// Indented code is accepted input but stored as fenced code.
	for i := 0; i < len(out); i++ {
		if !strings.HasPrefix(out[i], "    ") || (i > 0 && out[i-1] != "") {
			continue
		}
		end := i
		for end < len(out) && strings.HasPrefix(out[end], "    ") {
			end++
		}
		code := []string{"```"}
		for _, line := range out[i:end] {
			code = append(code, strings.TrimPrefix(line, "    "))
		}
		code = append(code, "```")
		out = append(append(out[:i:i], code...), out[end:]...)
		i += len(code) - 1
	}
	// Empty runs are one block separator. Preserving line indentation retains
	// list and table structure and makes a second pass byte-identical.
	compact := out[:0]
	for _, line := range out {
		if line != "" || len(compact) == 0 || compact[len(compact)-1] != "" {
			compact = append(compact, line)
		}
	}
	return strings.Join(compact, "\n") + "\n", nil
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
