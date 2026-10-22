package resource_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"golang.org/x/net/html"

	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestTheCreateLinkUsesTheRequestsLanguageAndSentenceCase(t *testing.T) {
	t.Parallel()
	catalog := xtext.Load("en",
		xtext.Source{Name: "resource", FS: resource.Catalogues()},
		xtext.Source{Name: "names", FS: fstest.MapFS{
			"en.json":    {Data: []byte(`{"resource.note.note":{"translation":"Note","plural":{"one":"Note","other":"Notes"}}}`)},
			"pt-PT.json": {Data: []byte(`{"resource.note.note":{"translation":"Nota","plural":{"one":"Nota","other":"Notas"}}}`)},
		}},
	)
	for _, language := range []struct{ tag, label string }{{"en", "New note"}, {"pt-PT", "Criar: nota"}} {
		t.Run(language.tag, func(t *testing.T) {
			o := opts
			o.Locale = new(catalog.Select(language.tag))
			for _, writable := range []bool{true, false} {
				// Matching row text cannot stand in for a missing or mislabelled link.
				rows := []map[string]any{{"id": "1", "title": language.label}}
				out := render(t, resource.List(note(), o, rows, 1, 1, "", writable).Body)
				doc, err := html.Parse(strings.NewReader(out))
				if err != nil {
					t.Fatal(err)
				}
				links := 0
				for n := range doc.Descendants() {
					if n.Type != html.ElementNode || n.Data != "a" {
						continue
					}
					for _, attr := range n.Attr {
						if attr.Key != "href" || attr.Val != "/app/note/notes/new" {
							continue
						}
						links++
						var label strings.Builder
						for child := range n.Descendants() {
							if child.Type == html.TextNode {
								label.WriteString(child.Data)
							}
						}
						if got := strings.TrimSpace(label.String()); got != language.label {
							t.Errorf("create link label = %q, want %q", got, language.label)
						}
					}
				}
				want := 0
				if writable {
					want = 1
				}
				if links != want {
					t.Errorf("writable=%t: got %d create links, want %d", writable, links, want)
				}
			}
		})
	}
}
