package history_test

import (
	"net/http"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// versionBlockText is the text of the panel's entry for one version — the element
// holding the restore form that posts that version — without the form itself.
func versionBlockText(panel *html.Node, version string) (string, bool) {
	var block *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if block != nil {
			return
		}
		if n.Type == html.ElementNode && n.Data == "input" {
			var name, value string
			for _, a := range n.Attr {
				switch a.Key {
				case "name":
					name = a.Val
				case "value":
					value = a.Val
				}
			}
			if name == "version" && value == version {
				form := n.Parent
				for form != nil && form.Data != "form" {
					form = form.Parent
				}
				if form != nil {
					block = form.Parent
				}
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(panel)
	if block == nil {
		return "", false
	}
	var b strings.Builder
	var text func(*html.Node)
	text = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "form" {
			return
		}
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			b.WriteString(" ")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			text(c)
		}
	}
	text(block)
	return b.String(), true
}

// A restore is a version like any other, and the panel says where it came from:
// the entry the restore wrote names the version it brought back, so the series
// reads "restored from v1" rather than an anonymous edit.
func TestTheHistoryPanelNamesTheVersionARestoreBroughtBack(t *testing.T) {
	_, router, created, screen := contentHistoryFixture(t)
	if code, body := call(t, router, acmeHost, http.MethodPost,
		"/api/v1/content/contents/"+created.ID.String()+"/restore",
		`{"version":1,"expectedRevision":2}`); code != http.StatusOK && code != http.StatusNoContent {
		t.Fatalf("restore version 1 = %d %s", code, body)
	}
	panel := servedHistoryPanel(t, router, screen+"/"+created.ID.String())
	entry, ok := versionBlockText(panel, "3")
	if !ok {
		t.Fatalf("the panel has no entry for version 3, the one the restore wrote")
	}
	if !strings.Contains(entry, "v1") {
		t.Errorf("the restore's entry reads %q and never names v1, the version it brought back", strings.Join(strings.Fields(entry), " "))
	}
}
