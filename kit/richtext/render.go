package richtext

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"

	"github.com/septagon-oss/platformkit/kit/db"
)

// Reference is one image occurrence, including repeats, in document order.
type Reference struct {
	ID           uuid.UUID
	Alt, Caption string
	Line         int
}

// References walks parsed image nodes; it never scans rendered HTML.
func References(d *Document) []Reference {
	if d == nil {
		return nil
	}
	var refs []Reference
	_ = ast.Walk(d.root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if img, ok := n.(*ast.Image); ok {
				match := imageURL.FindStringSubmatch(string(img.Destination))
				if len(match) == 2 {
					if id, err := uuid.Parse(match[1]); err == nil {
						refs = append(refs, Reference{id, nodeText(img, d.source), string(img.Title), d.line(img)})
					}
				}
			}
		}
		return ast.WalkContinue, nil
	})
	return refs
}

func nodeText(root ast.Node, source []byte) string {
	var b strings.Builder
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := n.(type) {
		case *ast.Text:
			b.Write(v.Segment.Value(source))
			if v.SoftLineBreak() || v.HardLineBreak() {
				b.WriteByte(' ')
			}
		case *ast.String:
			b.Write(v.Value)
		}
		return ast.WalkContinue, nil
	})
	return strings.Join(strings.Fields(b.String()), " ")
}

// PlainText extracts readable words from the syntax tree, including image descriptions.
func PlainText(d *Document) string {
	if d == nil {
		return ""
	}
	var b strings.Builder
	_ = ast.Walk(d.root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			if n.Type() == ast.TypeBlock {
				b.WriteByte(' ')
			}
			return ast.WalkContinue, nil
		}
		switch v := n.(type) {
		case *ast.Text:
			value := util.UnescapePunctuations(v.Segment.Value(d.source))
			value = util.ResolveNumericReferences(value)
			b.Write(util.ResolveEntityNames(value))
			if v.SoftLineBreak() || v.HardLineBreak() {
				b.WriteByte(' ')
			}
		case *ast.String:
			b.Write(v.Value)
		case *ast.AutoLink:
			b.Write(v.Label(d.source))
			return ast.WalkSkipChildren, nil
		case *ast.Image:
			b.WriteString(nodeText(v, d.source))
			if len(v.Title) > 0 {
				b.WriteByte(' ')
				b.Write(v.Title)
			}
			return ast.WalkSkipChildren, nil
		case *ast.FencedCodeBlock, *ast.CodeBlock:
			for i := 0; i < v.Lines().Len(); i++ {
				segment := v.Lines().At(i)
				b.Write(segment.Value(d.source))
			}
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return strings.Join(strings.Fields(b.String()), " ")
}

// MetaDescription truncates plain text on a word boundary, without an ellipsis.
func MetaDescription(d *Document, maxChars int) string {
	if maxChars <= 0 {
		return ""
	}
	runes := []rune(PlainText(d))
	if len(runes) <= maxChars {
		return string(runes)
	}
	end := maxChars
	for end > 0 && !unicode.IsSpace(runes[end]) {
		end--
	}
	if end == 0 {
		end = maxChars
	}
	return strings.TrimSpace(string(runes[:end]))
}

// FirstImage returns the first image occurrence, if any.
func FirstImage(d *Document) (Reference, bool) {
	refs := References(d)
	if len(refs) == 0 {
		return Reference{}, false
	}
	return refs[0], true
}

// Audience selects the request's authorized image URL.
type Audience uint8

const (
	Workspace Audience = iota
	Public
)

// Image is one validated first-party image representation.
type Image struct {
	Src, SrcSet   string
	Width, Height int

	// Sizes is the `sizes` attribute this representation asks the browser to lay
	// the frame out against, or empty for ProseSizes — the one value the format's
	// own prose column needs and the only one the sanitizer accepts. A port that
	// could return a `sizes` of its own choosing would be a port that could write
	// past the rule on the line above it.
	Sizes string
}

// ProseSizes is the one sizes attribute this format's prose column asks for: the
// figure is as wide as the text and never wider than the column. It is a
// constant rather than a per-image answer because the column is the format's, not
// the file's, and the writer below and the sanitizer's anchor both read this one
// string.
const ProseSizes = "(max-width: 65ch) 100vw, 65ch"

// ErrMissing hides absent, foreign, non-image and audience-inaccessible files alike.
var ErrMissing = errors.New("richtext: image unavailable")

// Files resolves an image inside the caller's typed tenant transaction.
type Files interface {
	Resolve(context.Context, db.Tx[db.Tenant], uuid.UUID, Audience) (Image, error)
}

// RejectImages is an explicit Files policy for compositions without file storage.
// Text-only richtext remains usable; every image reference is refused on write.
type RejectImages struct{}

func (RejectImages) Resolve(context.Context, db.Tx[db.Tenant], uuid.UUID, Audience) (Image, error) {
	return Image{}, ErrMissing
}

// Prepare is the shared write decision: validate, normalize, enforce length and resolve references.
func Prepare(ctx context.Context, tx db.Tx[db.Tenant], source string, files Files, maxChars int) (string, error) {
	normal, err := Normalise(source)
	if err != nil {
		return "", err
	}
	if maxChars > 0 && utf8.RuneCountInString(normal) > maxChars {
		return "", &Refused{Issues: []Issue{{
			Construct: "length", Line: 1,
			Remedy: fmt.Sprintf("Use at most %d Markdown characters.", maxChars), Class: Correctable,
		}}}
	}
	d, _ := Parse(normal)
	for _, ref := range References(d) {
		if files == nil {
			return "", fmt.Errorf("richtext: Files port is required")
		}
		image, err := files.Resolve(ctx, tx, ref.ID, Workspace)
		if errors.Is(err, ErrMissing) {
			return "", &Refused{Issues: []Issue{{
				Construct: "missing image", Line: ref.Line,
				Remedy: "Upload the image so it is stored with this site.", Class: Correctable,
			}}}
		}
		if err != nil {
			return "", fmt.Errorf("richtext: resolve image: %w", err)
		}
		if err := checkImage(image, ref.ID); err != nil {
			return "", err
		}
	}
	return normal, nil
}

// filePath is the first-party address this format will name, with or without one
// of the four planned widths on it. The width set is written out rather than
// matched as a number because it is a closed set: an address this kernel mounted
// and a width it never planned are the same string shape apart from the digits,
// and the digits are what the refusal is about.
var filePath = regexp.MustCompile(`^/api/v1/(?:file/files/([0-9a-f-]{36})/content|public/file/files/([0-9a-f-]{36}))(\?w=(?:480|960|1440|1920))?$`)

// srcsetEntry is one descriptor of a srcset: that same address, and a width.
var srcsetEntry = regexp.MustCompile(`^(/api/v1/(?:file/files/[0-9a-f-]{36}/content|public/file/files/[0-9a-f-]{36})(?:\?w=(?:480|960|1440|1920))?) ([1-9][0-9]*)w$`)

// checkImage is the write-time gate a stored body passes through, and the reason
// a srcset is safe to widen at all: every descriptor of every image of every body
// is asked here, at the moment the body is accepted, so the HTML a reader is
// served can name no address this kernel did not mount. Four things, of every
// descriptor:
//
//  1. its URL is exactly the image's own `src`, or that URL with `?w=` and its own
//     width on it — never another file, path or host, a scheme-relative //host or
//     a data: URL;
//  2. its width is positive and no larger than the frame's — a srcset promising
//     more pixels than the file holds is a lie to the browser;
//  3. no width appears twice, which is a list the browser cannot read;
//  4. exactly one descriptor carries no query and its width is the frame's — the
//     stored original, what a browser falls back to.
func checkImage(im Image, id uuid.UUID) error {
	if im.Width <= 0 || im.Height <= 0 || im.Width > 999999 || im.Height > 999999 {
		return fmt.Errorf("richtext: invalid file dimensions")
	}
	if im.Sizes != "" && im.Sizes != ProseSizes {
		return fmt.Errorf("richtext: an image provider returned sizes this site does not write")
	}
	m := filePath.FindStringSubmatch(im.Src)
	if len(m) != 4 || (m[1] != id.String() && m[2] != id.String()) {
		return fmt.Errorf("richtext: invalid file URL")
	}
	if im.SrcSet == "" {
		return fmt.Errorf("richtext: missing srcset")
	}
	seen := map[int]bool{}
	originals := 0
	for _, entry := range strings.Split(im.SrcSet, ", ") {
		parts := srcsetEntry.FindStringSubmatch(entry)
		if len(parts) != 3 {
			return fmt.Errorf("richtext: invalid srcset")
		}
		url, width := parts[1], parts[2]
		w, err := strconv.Atoi(width)
		if err != nil || w <= 0 || w > im.Width || seen[w] {
			return fmt.Errorf("richtext: invalid srcset")
		}
		seen[w] = true
		if url == im.Src {
			originals++
			if w != im.Width {
				return fmt.Errorf("richtext: invalid srcset")
			}
			continue
		}
		if url != im.Src+"?w="+width {
			return fmt.Errorf("richtext: invalid srcset")
		}
	}
	if originals != 1 {
		return fmt.Errorf("richtext: invalid srcset")
	}
	return nil
}

type figure struct {
	image        Image
	alt, caption string
	missing      bool
	first        bool
}
type proseRenderer struct {
	figures map[ast.Node]figure
	legacy  bool
}

func (r proseRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindParagraph, r.paragraph)
	reg.Register(ast.KindImage, r.image)
	reg.Register(ast.KindHeading, r.heading)
	reg.Register(ast.KindLink, r.link)
}

func (r proseRenderer) paragraph(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if f, ok := r.figures[n]; ok {
		if !entering {
			return ast.WalkContinue, nil
		}
		if f.missing {
			_, _ = w.WriteString(`<figure data-missing-image="true"><figcaption>Missing image</figcaption></figure>` + "\n")
			return ast.WalkSkipChildren, nil
		}
		loading := "lazy"
		if f.first {
			loading = "eager"
		}
		sizes := f.image.Sizes
		if sizes == "" {
			sizes = ProseSizes
		}
		_, _ = fmt.Fprintf(w, `<figure><img src="%s" srcset="%s" width="%d" height="%d" sizes="%s" alt="%s" loading="%s" decoding="async">`, html.EscapeString(f.image.Src), html.EscapeString(f.image.SrcSet), f.image.Width, f.image.Height, html.EscapeString(sizes), html.EscapeString(f.alt), loading)
		if f.caption != "" {
			_, _ = w.WriteString("<figcaption>" + html.EscapeString(f.caption) + "</figcaption>")
		}
		_, _ = w.WriteString("</figure>\n")
		return ast.WalkSkipChildren, nil
	}
	if entering {
		_, _ = w.WriteString("<p>")
	} else {
		_, _ = w.WriteString("</p>\n")
	}
	return ast.WalkContinue, nil
}

func (r proseRenderer) image(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		_, _ = w.WriteString(html.EscapeString(nodeText(n, source)))
		return ast.WalkSkipChildren, nil
	}
	return ast.WalkContinue, nil
}

func (r proseRenderer) heading(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	h := n.(*ast.Heading)
	if entering {
		id := ""
		if v, ok := n.AttributeString("id"); ok {
			switch value := v.(type) {
			case []byte:
				id = string(value)
			case string:
				id = value
			}
		}
		id = strings.TrimPrefix(id, "pk-")
		id = regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(strings.ToLower(id), "-")
		_, _ = fmt.Fprintf(w, `<h%d id="pk-%s">`, h.Level, html.EscapeString(id))
	} else {
		_, _ = fmt.Fprintf(w, "</h%d>\n", h.Level)
	}
	return ast.WalkContinue, nil
}

func (r proseRenderer) link(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		dest := string(n.(*ast.Link).Destination)
		if !validLink(dest) && r.legacy && strings.HasPrefix(dest, "//") {
			dest = "https:" + dest
		}
		if !validLink(dest) {
			return ast.WalkContinue, nil
		}
		// Validation accepts schemes case insensitively. The final sanitizer's
		// allowlist uses lowercase spellings, so give it that same spelling.
		if scheme, rest, ok := strings.Cut(dest, ":"); ok {
			dest = strings.ToLower(scheme) + ":" + rest
		}
		_, _ = fmt.Fprintf(w, `<a href="%s"`, html.EscapeString(dest))
		// The author's own words about where the address goes are part of what they
		// wrote, so they are rendered: dropped here, a link saved with a title would come
		// back from the store and be shown without it, and no further write could put it
		// back. The backslashes that let a quote sit inside a title are read the same way
		// they are in the text of a paragraph, and the phrase reaches the page through the
		// escaping alt text is under — the policy beside this one allows the attribute and
		// decides nothing about its words, because a title is nobody's closed set.
		if title := string(util.UnescapePunctuations(n.(*ast.Link).Title)); title != "" {
			_, _ = fmt.Fprintf(w, ` title="%s"`, html.EscapeString(title))
		}
		if strings.HasPrefix(dest, "http://") || strings.HasPrefix(dest, "https://") {
			_, _ = w.WriteString(` rel="noopener noreferrer nofollow ugc"`)
		}
		_, _ = w.WriteString(">")
	} else {
		_, _ = w.WriteString("</a>")
	}
	return ast.WalkContinue, nil
}

func richTextPolicy(legacy bool) *bluemonday.Policy {
	p := bluemonday.StrictPolicy()
	p.AllowElements("p", "br", "h2", "h3", "h4", "strong", "em", "del", "code", "pre", "blockquote", "hr", "ul", "ol", "li", "input", "a", "table", "thead", "tbody", "tr", "th", "td", "figure", "figcaption", "img")
	if legacy {
		p.AllowElements("h1", "h5", "h6")
	}
	p.RequireParseableURLs(true)
	p.AllowURLSchemes("http", "https", "mailto", "tel")
	p.AllowRelativeURLs(true)
	p.AllowAttrs("href").Matching(regexp.MustCompile(`^(?:https?://[^[:space:]<>"']+|mailto:[^[:space:]<>"']+|tel:[+0-9().-]+|/(?:[^/\\[:space:]<>"'][^\\[:space:]<>"']*)?|#[A-Za-z0-9_-]+)$`)).OnElements("a")
	p.AllowAttrs("rel").Matching(regexp.MustCompile(`^noopener noreferrer nofollow ugc$`)).OnElements("a")
	// A link's title is the author's sentence about the destination, which is why the
	// stored form carries it. Its length and words are nobody's to decide, and the
	// value reaches the page through the same escaping alt text does.
	p.AllowAttrs("title").OnElements("a")
	p.AllowAttrs("id").Matching(regexp.MustCompile(`^pk-[a-z0-9-]+$`)).OnElements("h2", "h3", "h4")
	p.AllowAttrs("class").Matching(regexp.MustCompile(`^language-[A-Za-z0-9_+-]+$`)).OnElements("code")
	p.AllowAttrs("align").Matching(regexp.MustCompile(`^(left|center|right)$`)).OnElements("th", "td")
	p.AllowAttrs("type").Matching(regexp.MustCompile(`^checkbox$`)).OnElements("input")
	p.AllowAttrs("disabled", "checked").OnElements("input")
	path := `/api/v1/(?:file/files/[0-9a-f-]{36}/content|public/file/files/[0-9a-f-]{36})`
	// The width a srcset candidate names, as the closed set it is. Same argument
	// as checkImage's: the digits are what the refusal is about, and a policy that
	// matched any `?w=` would let a stored body name an address this kernel
	// refuses to serve.
	width := `(?:\?w=(?:480|960|1440|1920))?`
	p.AllowAttrs("src").Matching(regexp.MustCompile(`^` + path + `$`)).OnElements("img")
	p.AllowAttrs("srcset").Matching(regexp.MustCompile(`^` + path + width + ` [1-9][0-9]*w(?:, ` + path + width + ` [1-9][0-9]*w)*$`)).OnElements("img")
	p.AllowAttrs("width", "height").Matching(regexp.MustCompile(`^[1-9][0-9]{0,5}$`)).OnElements("img")
	p.AllowAttrs("sizes").Matching(regexp.MustCompile(`^\(max-width: 65ch\) 100vw, 65ch$`)).OnElements("img")
	p.AllowAttrs("alt").OnElements("img")
	p.AllowAttrs("loading").Matching(regexp.MustCompile(`^(lazy|eager)$`)).OnElements("img")
	p.AllowAttrs("decoding").Matching(regexp.MustCompile(`^async$`)).OnElements("img")
	p.AllowAttrs("data-missing-image").Matching(regexp.MustCompile(`^true$`)).OnElements("figure")
	return p
}

// Render resolves images for an audience and returns sanitized HTML.
func Render(ctx context.Context, tx db.Tx[db.Tenant], doc *Document, files Files, audience Audience) (string, error) {
	return render(ctx, tx, doc, files, audience, false)
}

func render(ctx context.Context, tx db.Tx[db.Tenant], doc *Document, files Files, audience Audience, legacy bool) (string, error) {
	if doc == nil {
		return "", fmt.Errorf("richtext: nil document")
	}
	// A fresh AST keeps the caller's parsed value immutable across concurrent reads.
	d, err := Parse(string(doc.source))
	if err != nil {
		return "", err
	}
	figures := make(map[ast.Node]figure)
	first := true
	var resolveErr error
	_ = ast.Walk(d.root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		p, ok := n.(*ast.Paragraph)
		if !ok {
			return ast.WalkContinue, nil
		}
		im, ok := p.FirstChild().(*ast.Image)
		if !ok || im.NextSibling() != nil {
			return ast.WalkContinue, nil
		}
		match := imageURL.FindStringSubmatch(string(im.Destination))
		if len(match) != 2 {
			return ast.WalkContinue, nil
		}
		id, err := uuid.Parse(match[1])
		if err != nil {
			return ast.WalkContinue, nil
		}
		f := figure{alt: nodeText(im, d.source), caption: string(im.Title), missing: true, first: first}
		if files != nil {
			image, err := files.Resolve(ctx, tx, id, audience)
			if err == nil {
				err = checkImage(image, id)
				if err == nil {
					f.image, f.missing = image, false
				}
			}
			if err != nil && !errors.Is(err, ErrMissing) {
				resolveErr = err
				return ast.WalkStop, err
			}
		}
		figures[p] = f
		first = false
		return ast.WalkSkipChildren, nil
	})
	if resolveErr != nil {
		return "", fmt.Errorf("richtext: resolve image: %w", resolveErr)
	}
	engine := goldmark.New(goldmark.WithExtensions(extension.GFM), goldmark.WithParserOptions(parser.WithAutoHeadingID()), goldmark.WithRendererOptions(renderer.WithNodeRenderers(util.Prioritized(proseRenderer{figures, legacy}, 500))))
	var b bytes.Buffer
	if err := engine.Renderer().Render(&b, d.source, d.root); err != nil {
		return "", err
	}
	return richTextPolicy(legacy).Sanitize(b.String()), nil
}

// RenderLegacy safely reads content saved before rich-text write validation.
func RenderLegacy(source string) (string, error) {
	d, err := Parse(source)
	if err != nil {
		return "", err
	}
	return render(context.Background(), db.Tx[db.Tenant]{}, d, nil, Workspace, true)
}
