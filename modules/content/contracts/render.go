package contracts

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"github.com/septagon-oss/platformkit/kit/crud"
)

// markdown is the renderer. GFM is what people who write Markdown already know
// — tables, strikethrough, task lists, autolinks — and the heading ids are what
// a table of contents links to.
//
// html.WithUnsafe is deliberately absent, which is the first of the two layers:
// goldmark leaves raw HTML out of its output entirely rather than passing it
// through, so a <script> somebody typed into the body never reaches the
// sanitizer at all.
var markdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(
		parser.WithAutoHeadingID(),
		parser.WithASTTransformers(util.Prioritized(explicitLinks{}, 100)),
	),
)

// explicitLinks makes every protocol-relative destination say which protocol it
// means.
//
// `[click](//evil.example)` is a link to another origin that reads, in the
// source and in the rendered anchor, like a path within this site — and
// bluemonday's URL policy does not object, because a network-path reference is
// a relative URL and relative URLs are what a site's own links are. So it is
// rewritten to https://evil.example, which goes exactly where it said and is
// now visibly somebody else's. `/\evil.example` is the same trick spelled with
// the character every browser normalises to a slash.
//
// Rewriting rather than refusing, because Render runs on read: refusing would
// turn a body written before this rule into a page that cannot be served.
type explicitLinks struct{}

func (explicitLinks) Transform(doc *ast.Document, _ text.Reader, _ parser.Context) {
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := n.(type) {
		case *ast.Link:
			v.Destination = explicit(v.Destination)
		case *ast.Image:
			v.Destination = explicit(v.Destination)
		}
		return ast.WalkContinue, nil
	})
}

// explicit is one destination, with a protocol-relative one made absolute. A
// destination that is an ordinary path, an ordinary absolute URL or an anchor
// is returned unchanged.
func explicit(dst []byte) []byte {
	s := string(dst)
	if len(s) < 2 || s[0] != '/' || (s[1] != '/' && s[1] != '\\') {
		return dst
	}
	return []byte("https://" + strings.TrimLeft(s, "/\\"))
}

// policy is the second layer: bluemonday's user-generated-content policy, which
// allows the elements a document is made of and no event handlers, no style, no
// script, no iframe, and no javascript: URL.
//
// Two layers for one job, and it is worth saying why rather than trusting one.
// The renderer's job is to produce HTML and the sanitizer's is to distrust HTML,
// and they fail differently: an extension somebody adds to the first can start
// emitting raw HTML, and a body row written before the policy tightened is
// still in the database. Rendering on read rather than on write is the other
// half of that — there is no stored HTML column to go stale, so a policy change
// applies to everything ever written.
var policy = bluemonday.UGCPolicy()

// Render turns a body into the HTML the public route serves. A body that
// goldmark cannot render at all is an invalid entity rather than an empty page:
// serving nothing where a document should be is the failure nobody notices.
func Render(body string) (string, error) {
	var out bytes.Buffer
	if err := markdown.Convert([]byte(body), &out); err != nil {
		return "", fmt.Errorf("%w: this content is not Markdown this renderer can read: %s", crud.ErrInvalid, err)
	}
	return policy.Sanitize(out.String()), nil
}
