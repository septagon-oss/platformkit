package history_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

// servedHistoryPanel is the History disclosure of the served detail page, the
// element a person opens; it fails the test when the page offers none.
func servedHistoryPanel(t *testing.T, router http.Handler, path string) *html.Node {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "http://"+acmeHost+path, nil)
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail = %d %s", rec.Code, rec.Body.String())
	}
	doc, err := html.Parse(strings.NewReader(rec.Body.String()))
	if err != nil {
		t.Fatal(err)
	}
	var panel *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if panel == nil && n.Type == html.ElementNode && n.Data == "details" {
			panel = n
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	if panel == nil {
		t.Fatalf("the versioned detail offers no History disclosure")
	}
	return panel
}

// elementText is every text under the elements named tag inside n, joined.
func elementText(n *html.Node, tag string) string {
	var b strings.Builder
	var walk func(*html.Node, bool)
	walk = func(n *html.Node, inside bool) {
		inside = inside || n.Type == html.ElementNode && n.Data == tag
		if inside && n.Type == html.TextNode {
			b.WriteString(n.Data)
			b.WriteString(" ")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, inside)
		}
	}
	walk(n, false)
	return b.String()
}

// The panel compares: selecting a version shows, in the rendered prose, the words
// the write inserted as an insertion and the words it removed as a deletion, so a
// person sees what changed rather than two values side by side.
func TestTheHistoryPanelMarksTheWordsAnEditInsertedAndRemoved(t *testing.T) {
	_, router, created, screen := contentHistoryFixture(t)
	panel := servedHistoryPanel(t, router, screen+"/"+created.ID.String())
	if inserted := elementText(panel, "ins"); !strings.Contains(inserted, "Current") {
		t.Errorf("the panel marks %q as inserted, want the edit's word \"Current\"", inserted)
	}
	if removed := elementText(panel, "del"); !strings.Contains(removed, "Earlier") {
		t.Errorf("the panel marks %q as removed, want the replaced word \"Earlier\"", removed)
	}
}
