package main

import (
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
	"golang.org/x/net/html"
)

// The record's Translate form is a door a person walks, not a description of one:
// what the screen offers must arrive at the command as the text of the field the
// editor typed, in the language the form is for, at the revision the screen read.
// A form that renders a language and no text can only answer 422 to a person who
// has every grant and an untranslated record, which is the failure this pins shut.
func TestTheRecordsTranslateFormWritesWhatTheEditorTyped(t *testing.T) {
	cfg, who, id := publishedTranslationPage(t, nil)
	at := "/app/content/contents/" + id + "/translate"
	form := url.Values{"lang": {"pt-PT"}, "values[title]": {"A nossa história"}, "expected[title]": {"0"}}
	status, body := postForm(t, cfg, who, acmeHost, at, form)
	if status != http.StatusSeeOther {
		t.Fatalf("translating from the record's form = %d %.300s; want the redirect the screen answers with", status, body)
	}
	status, body = do(t, cfg, who, http.MethodGet, acmeHost, contentPath+"/"+id+"?lang=pt-PT", "")
	if status != http.StatusOK || !strings.Contains(body, "A nossa história") {
		t.Fatalf("the record after the form's write = %d %s; want the Portuguese the editor typed", status, body)
	}
	// The revision the form carried was read before the row existed, and the save
	// above moved it. The same body again is a write over text somebody has since
	// written, and the answer is a refusal that changed nothing.
	if status, _ = postForm(t, cfg, who, acmeHost, at, form); status != http.StatusConflict {
		t.Fatalf("the same form posted again = %d; want the conflict the stale revision is there to cause", status)
	}
	status, body = do(t, cfg, who, http.MethodGet, acmeHost, contentPath+"/"+id+"?lang=pt-PT", "")
	if strings.Count(body, "A nossa história") != 1 {
		t.Fatalf("the record after the refused write = %d %s", status, body)
	}
}

// TestTheRecordsTranslateFormIsOfferedPerLanguage pins the shape the editor reads:
// one form per language the tenant is served in besides its own, each naming the
// language it writes rather than asking for it to be typed.
func TestTheRecordsTranslateFormIsOfferedPerLanguage(t *testing.T) {
	cfg, who, id := publishedTranslationPage(t, nil)
	status, body := do(t, cfg, who, http.MethodGet, acmeHost, "/app/content/contents/"+id, "")
	if status != http.StatusOK {
		t.Fatalf("record screen = %d", status)
	}
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var actions, named []string
	var lang string
	var forms func(*html.Node, bool)
	forms = func(n *html.Node, translate bool) {
		inner := translate
		if n.Type == html.ElementNode {
			switch n.Data {
			case "form":
				inner = attr(n, "action") == "/app/content/contents/"+id+"/translate"
				if inner {
					actions = append(actions, attr(n, "aria-label"))
				}
			case "input", "textarea":
				if !translate {
					break
				}
				name := attr(n, "name")
				if name != "" && attr(n, "type") != "hidden" {
					named = append(named, name)
				}
				if name == "lang" {
					lang = attr(n, "value")
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			forms(child, inner)
		}
	}
	forms(doc, false)
	if len(actions) != 1 {
		t.Fatalf("the record offers %d translate forms (%v); want one for pt-PT", len(actions), actions)
	}
	if lang != "pt-PT" {
		t.Fatalf("the translate form carries lang %q; want the language it is offered for", lang)
	}
	for _, want := range []string{"values[title]", "values[body]"} {
		if !slices.Contains(named, want) {
			t.Fatalf("the translate form offers %v; it is missing %s", named, want)
		}
	}
	for _, unwanted := range []string{"expected[title]", "expected[body]"} {
		if slices.Contains(named, unwanted) {
			t.Fatalf("the translate form asks the editor for %s; a revision is what the screen carries", unwanted)
		}
	}
	if !strings.Contains(actions[0], "pt-PT") {
		t.Fatalf("the translate form is named %q; it has to say which language it writes", actions[0])
	}
}

// postForm is a person pressing the screen's button: the browser's own encoding,
// with no JSON in sight, and no redirect followed, because the answer the screen
// gives a write is where it sent the person afterwards.
func postForm(t *testing.T, cfg config.Config, client *http.Client, host, path string, form url.Values) (int, string) {
	t.Helper()
	noRedirect := *client
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client = &noRedirect
	req, err := http.NewRequest(http.MethodPost, "http://"+cfg.Server.Addr+path, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = host
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://"+host)
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(body)
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
