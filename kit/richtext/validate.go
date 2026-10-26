package richtext

import (
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
)

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
		if entering {
			validateNode(d, n, add)
		}
		return ast.WalkContinue, nil
	})
	prose := maskedProse(d)
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

func validateNode(d *Document, n ast.Node, add func(ast.Node, string, string)) {
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
		validateImage(n, v, add)
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
}

func validateImage(n ast.Node, v *ast.Image, add func(ast.Node, string, string)) {
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
}

func maskedProse(d *Document) []byte {
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
	// Blank escaped punctuation without changing line positions. Paired
	// backslashes escape each other, so following syntax remains visible.
	for i := 0; i < len(prose)-1; i++ {
		if prose[i] == '\\' && isPunctuation(prose[i+1]) {
			prose[i], prose[i+1] = ' ', ' '
			i++
		}
	}
	return prose
}
