package main

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// A translation command needs the record's text as well as a language. Rendering
// its language alone leaves a form that can only refuse, even for an editor who
// has every required grant and a record with no existing translation.
func TestTranslationFormOffersTextToTranslate(t *testing.T) {
	cfg, who, id := publishedTranslationPage(t, nil)
	status, body := do(t, cfg, who, http.MethodGet, acmeHost, "/app/content/contents/"+id, "")
	if status != http.StatusOK || !strings.Contains(body, "Our story") {
		t.Fatalf("record screen = %d; want the editor's record", status)
	}
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var form *html.Node
	var find func(*html.Node)
	find = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "form" {
			for _, a := range n.Attr {
				if a.Key == "action" && strings.HasSuffix(a.Val, "/"+id+"/translate") {
					form = n
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			find(child)
		}
	}
	find(doc)
	if form == nil {
		t.Fatal("the record offers no translation form for its text")
	}
	var editable []string
	var controls func(*html.Node)
	controls = func(n *html.Node) {
		if n.Type == html.ElementNode && slices.Contains([]string{"input", "textarea", "select"}, n.Data) {
			var name, kind string
			locked := false
			for _, a := range n.Attr {
				switch a.Key {
				case "name":
					name = a.Val
				case "type":
					kind = a.Val
				case "disabled", "readonly":
					locked = true
				}
			}
			if name != "" && kind != "hidden" && kind != "submit" && !locked {
				editable = append(editable, name)
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			controls(child)
		}
	}
	controls(form)
	// Accept a map editor or derived field editors; neither the current missing
	// controls nor a particular future field-name convention is the oracle.
	for _, name := range editable {
		if name == "values" || strings.Contains(name, "title") || strings.Contains(name, "body") {
			return
		}
	}
	t.Fatalf("Translate offers editable controls %v; none accepts the record's translated text", editable)
}
