package page_test

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/ui/page"
)

func TestDocumentLanguageFollowsTheRenderedView(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, view, want string
		attrs            map[string]string
	}{
		{name: "existing English default", want: "en"},
		{name: "Portuguese view", view: "pt-PT", want: "pt-PT"},
		{name: "script and region", view: "zh-Hant-TW", want: "zh-Hant-TW"},
		{name: "shell default", attrs: map[string]string{"lang": "pt-PT"}, want: "pt-PT"},
		{name: "view overrides shell", attrs: map[string]string{"lang": "pt-PT"}, view: "en-GB", want: "en-GB"},
		{name: "empty shell default", attrs: map[string]string{"lang": ""}, want: "en"},
		{name: "case-insensitive HTML attribute", attrs: map[string]string{"LANG": "pt-PT"}, want: "pt-PT"},
		{name: "canonical shell attribute wins", attrs: map[string]string{"LANG": "de", "lang": "pt-PT"}, want: "pt-PT"},
		{name: "one language with conflicting attributes", attrs: map[string]string{"LANG": "de", "lang": "pt-PT"}, view: "fr", want: "fr"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := chrome()
			for key, value := range tc.attrs {
				c.Attrs[key] = value
			}
			output := render(t, page.Document(c, page.Request{}, page.View{Language: tc.view}, h.Main()))
			root := parseDocument(t, output)
			var languages []string
			for _, attribute := range root.Attr {
				if attribute.Key == "lang" {
					languages = append(languages, attribute.Val)
				}
			}
			if len(languages) != 1 || languages[0] != tc.want {
				t.Fatalf("document languages = %v, want exactly %q", languages, tc.want)
			}
			for key, value := range tc.attrs {
				if c.Attrs[key] != value {
					t.Fatalf("shared chrome attribute %s was mutated", key)
				}
			}
		})
	}
	// One request's choice must not become the next request's default.
	c := chrome()
	_ = render(t, page.Document(c, page.Request{}, page.View{Language: "pt-PT"}, h.Main()))
	root := parseDocument(t, render(t, page.Document(c, page.Request{}, page.View{}, h.Main())))
	if root.Attr[0].Key != "lang" || root.Attr[0].Val != "en" {
		t.Fatal("view language leaked into the shared chrome")
	}
}

func TestEnglishRecoveryCopyKeepsItsLanguageInAPortugueseDocument(t *testing.T) {
	t.Parallel()
	c := chrome()
	c.Attrs["lang"] = "pt-PT"
	c.Scripts = append(c.Scripts, "htmx-config.js")
	root := parseDocument(t, render(t, page.Document(c, page.Request{}, page.View{}, h.Main(g.Text("Operações")))))
	var notices int
	var visit func(*html.Node, string)
	visit = func(node *html.Node, language string) {
		for _, a := range node.Attr {
			if a.Key == "lang" {
				language = a.Val
			}
		}
		for _, a := range node.Attr {
			if a.Key == "data-request-notice" {
				notices++
				if language != "en" {
					t.Errorf("English recovery notice inherited %q", language)
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child, language)
		}
	}
	visit(root, "")
	if notices != 4 {
		t.Fatalf("recovery notices = %d, want 4", notices)
	}
	fault := page.Fault(404, "", "/", "Home")
	root = parseDocument(t, render(t, page.Document(c, page.Request{}, fault, g.Group(fault.Body))))
	if root.Attr[0].Key != "lang" || root.Attr[0].Val != "en" {
		t.Fatal("English generated fault inherited the shell's Portuguese language")
	}
}

func parseDocument(t *testing.T, source string) *html.Node {
	t.Helper()
	document, err := html.Parse(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	for node := document.FirstChild; node != nil; node = node.NextSibling {
		if node.Type == html.ElementNode && node.Data == "html" {
			return node
		}
	}
	t.Fatal("rendered document has no html element")
	return nil
}
