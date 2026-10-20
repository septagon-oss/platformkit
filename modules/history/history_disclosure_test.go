package history_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/net/html"
	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/screens"
)

func TestAVersionedDetailOffersAHistoryDisclosure(t *testing.T) {
	w := newWorld(t)
	api, router := mountedHistorySpec(t, w, rest.Spec[*Memo]{
		Module: "memos", Entity: "memo", Path: "/memo", Read: memoRead, Write: memoWrite,
		History: true, Versions: w.store, Immutable: []string{"status"},
	})
	id := create(t, router, acmeHost, "First draft")
	if code, body := edit(t, router, acmeHost, id, "Second draft"); code != http.StatusOK {
		t.Fatalf("edit = %d %s", code, body)
	}
	resources := api.Resources()
	if len(resources) != 1 {
		t.Fatalf("mounted %d resources, want the memo", len(resources))
	}
	res := resources[0]
	screens.Mount(api.Surfaces(res.Module).App, page.Shell{
		Frame: func(_ context.Context, _ page.Request, body []g.Node) g.Node { return g.Group(body) },
	}, screens.Options{Workspace: "/app", Home: "Dashboard"}, res)
	code, body := call(t, router, acmeHost, http.MethodGet, res.Screen+"/"+id.String(), "")
	if code != http.StatusOK || !strings.Contains(body, "Second draft") {
		t.Fatalf("detail did not show the current record: %d %s", code, body)
	}
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var hasHistory bool
	var walk func(*html.Node, bool)
	walk = func(n *html.Node, inSummary bool) {
		inSummary = inSummary || n.Type == html.ElementNode && n.Data == "summary"
		if inSummary && n.Type == html.TextNode && strings.Contains(n.Data, "History") {
			hasHistory = true
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child, inSummary)
		}
	}
	walk(doc, false)
	if !hasHistory {
		t.Error("the versioned detail has no History disclosure to select, compare or restore an earlier version")
	}
}
