package richtext

import (
	"context"
	"errors"
	xhtml "golang.org/x/net/html"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
)

type testFiles struct {
	image Image
	err   error
}

func (f testFiles) Resolve(_ context.Context, _ db.Tx[db.Tenant], _ uuid.UUID, _ Audience) (Image, error) {
	return f.image, f.err
}

func TestRenderAndExtract(t *testing.T) {
	source := "## Opening hours\n\n- Monday\n- Tuesday\n\n[Website](https://example.test)\n\n```go\nfmt.Println(1)\n```"
	d, err := Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Render(context.Background(), db.Tx[db.Tenant]{}, d, nil, Workspace)
	if err != nil {
		t.Fatal(err)
	}
	wantGolden, err := os.ReadFile("testdata/opening_hours.html")
	if err != nil {
		t.Fatal(err)
	}
	if got != string(wantGolden) {
		t.Fatalf("rendered prose differs from golden:\n%s\nwant:\n%s", got, wantGolden)
	}
	for _, want := range []string{`id="pk-opening-hours"`, "<ul>", `rel="noopener noreferrer nofollow ugc"`, `<code class="language-go">`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	if strings.ContainsAny(PlainText(d), "<>*#`") {
		t.Fatalf("markup in plain text: %q", PlainText(d))
	}
	if desc := MetaDescription(d, 15); desc != "Opening hours" {
		t.Errorf("description = %q", desc)
	}
	long, err := Parse(strings.Repeat("word ", 39) + "last")
	if err != nil {
		t.Fatal(err)
	}
	if desc := MetaDescription(long, 160); len([]rune(desc)) > 160 || strings.HasSuffix(desc, "wor") {
		t.Errorf("meta description breaks a word: %q", desc)
	}
}

func TestImageResolutionAndPolicy(t *testing.T) {
	id := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	source := "![Alt](" + testImage + ` "Caption")`
	d, err := Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	refs := References(d)
	if len(refs) != 1 || refs[0].ID != id || refs[0].Alt != "Alt" || refs[0].Caption != "Caption" {
		t.Fatalf("references: %+v", refs)
	}
	if first, ok := FirstImage(d); !ok || first.ID != id {
		t.Fatalf("first image: %+v, %v", first, ok)
	}
	path := "/api/v1/file/files/" + id.String() + "/content"
	files := testFiles{image: Image{path, path + " 800w", 800, 600}}
	if _, err := Prepare(context.Background(), db.Tx[db.Tenant]{}, source, files, 100); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	html, err := Render(context.Background(), db.Tx[db.Tenant]{}, d, files, Workspace)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<figure>", "<img", `srcset="` + path + ` 800w"`, "<figcaption>Caption</figcaption>"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in %s", want, html)
		}
	}
	missing := testFiles{err: ErrMissing}
	if _, err := Prepare(context.Background(), db.Tx[db.Tenant]{}, source, missing, 100); err == nil {
		t.Fatal("accepted missing image")
	}
	html, err = Render(context.Background(), db.Tx[db.Tenant]{}, d, missing, Workspace)
	if err != nil || !strings.Contains(html, "Missing image") || strings.Contains(html, "<img") {
		t.Fatalf("missing: %q, %v", html, err)
	}
	bad := testFiles{image: Image{"https://attacker.test/x", "https://attacker.test/x 800w", 800, 600}}
	if _, err := Prepare(context.Background(), db.Tx[db.Tenant]{}, source, bad, 100); err == nil {
		t.Fatal("accepted off-site image")
	}
	if html, err := Render(context.Background(), db.Tx[db.Tenant]{}, d, bad, Workspace); err == nil || html != "" {
		t.Fatalf("off-site render: %q, %v", html, err)
	}
	down := testFiles{err: errors.New("storage offline")}
	if html, err := Render(context.Background(), db.Tx[db.Tenant]{}, d, down, Workspace); err == nil || html != "" {
		t.Fatalf("outage: %q, %v", html, err)
	}
}

func FuzzRenderSafe(f *testing.F) {
	for _, s := range []string{"plain", "<script>x</script>", "![x](https://evil.test/x)", "[bad](javascript:alert(1))", "## Heading", "![x](" + testImage + ")"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, source string) {
		normal, err := Normalise(source)
		if err != nil {
			return
		}
		d, err := Parse(normal)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Render(context.Background(), db.Tx[db.Tenant]{}, d, testFiles{err: ErrMissing}, Workspace)
		if err != nil {
			t.Fatal(err)
		}
		lower := strings.ToLower(got)
		for _, forbidden := range []string{"<script", `href="javascript:`, `src="javascript:`, `href="data:`, `src="data:`, " onload=", " onclick=", "<img"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("unsafe %q: %s", forbidden, got)
			}
		}
	})
}

func FuzzPlainTextNoMarkup(f *testing.F) {
	for _, s := range []string{"plain", "**bold**", "## Heading", "[link](/help)", "<script>x</script>"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, source string) {
		normal, err := Normalise(source)
		if err != nil {
			return
		}
		d, _ := Parse(normal)
		if len(References(d)) > 0 {
			return
		} // missing-image fallback has its own visible text
		got := PlainText(d)
		rendered, err := RenderLegacy(normal)
		if err != nil {
			t.Fatal(err)
		}
		root, err := xhtml.Parse(strings.NewReader(rendered))
		if err != nil {
			t.Fatal(err)
		}
		var visible strings.Builder
		var walk func(*xhtml.Node)
		walk = func(n *xhtml.Node) {
			if n.Type == xhtml.TextNode {
				visible.WriteString(n.Data)
			}
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
			if n.Type == xhtml.ElementNode {
				switch n.Data {
				case "p", "div", "blockquote", "li", "ul", "ol", "pre", "h1", "h2", "h3", "h4", "h5", "h6", "table", "tr", "td", "th":
					visible.WriteByte(' ')
				}
			}
		}
		walk(root)
		want := strings.Join(strings.Fields(visible.String()), " ")
		if got != want {
			t.Fatalf("plain text %q differs from rendered text nodes %q for %q", got, want, normal)
		}
	})
}
