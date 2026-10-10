package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// The form a browser posts is the form the screen rendered, every control of it:
// the hidden lang, a hidden expected[<field>] per field, and one box per field of
// which the editor filled exactly one. The empty box is a field left alone, not a
// field written blank — the typed title lands in Portuguese and the body still
// falls back to its English source, named in _i18n. A test that hand-builds the
// body instead would keep passing while the rendered controls and the form
// decoder drifted apart.
func TestAnEmptyBoxOnTheTranslateFormLeavesTheFieldUntouched(t *testing.T) {
	cfg, who, id := publishedTranslationPage(t, nil)
	status, body := do(t, cfg, who, http.MethodGet, acmeHost, "/app/content/contents/"+id, "")
	if status != http.StatusOK {
		t.Fatalf("record screen = %d", status)
	}
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	action := "/app/content/contents/" + id + "/translate"
	form := url.Values{}
	var collect func(*html.Node, bool)
	collect = func(n *html.Node, inside bool) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "form":
				inside = attr(n, "action") == action
			case "input":
				if inside && attr(n, "name") != "" && attr(n, "type") != "submit" {
					form.Add(attr(n, "name"), attr(n, "value"))
				}
			case "textarea":
				if inside && attr(n, "name") != "" {
					text := ""
					if n.FirstChild != nil && n.FirstChild.Type == html.TextNode {
						text = n.FirstChild.Data
					}
					form.Add(attr(n, "name"), text)
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			collect(child, inside)
		}
	}
	collect(doc, false)
	if len(form) == 0 {
		t.Fatal("the record renders no translate form to post")
	}
	form.Set("values[title]", "A nossa história")
	status, body = postForm(t, cfg, who, acmeHost, action, form)
	if status != http.StatusSeeOther {
		t.Fatalf("the rendered form posted as a browser posts it = %d %.300s; want the redirect", status, body)
	}
	status, body = do(t, cfg, who, http.MethodGet, acmeHost, contentPath+"/"+id+"?lang=pt-PT", "")
	if status != http.StatusOK || !strings.Contains(body, "A nossa história") {
		t.Fatalf("the record after the write = %d %s; want the Portuguese the editor typed", status, body)
	}
	if !strings.Contains(body, "The original English paragraph.") {
		t.Errorf("the box the editor left empty must leave the body falling back to its source: %s", body)
	}
	var record struct {
		I18n map[string]any `json:"_i18n"`
	}
	if err := json.Unmarshal([]byte(body), &record); err != nil {
		t.Fatalf("the translated read is not the object it promises: %v in %s", err, body)
	}
	if _, fell := record.I18n["body"]; !fell {
		t.Errorf("_i18n = %v; want the untouched body named as fallen back", record.I18n)
	}
	if _, fell := record.I18n["title"]; fell {
		t.Errorf("_i18n = %v; the typed title is in the requested language and must not be named", record.I18n)
	}
}
