package history_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// restoreForms reads every restore form the served detail page offers, keyed by the
// version it posts, as the url-encoded body a browser would submit.
func restoreForms(t *testing.T, body string) map[string]url.Values {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	forms := map[string]url.Values{}
	var walk func(n *html.Node, form url.Values)
	walk = func(n *html.Node, form url.Values) {
		if n.Type == html.ElementNode && n.Data == "form" {
			for _, a := range n.Attr {
				if a.Key == "action" && strings.HasSuffix(a.Val, "/restore") {
					form = url.Values{}
				}
			}
		}
		if form != nil && n.Type == html.ElementNode && n.Data == "input" {
			var name, value string
			for _, a := range n.Attr {
				switch a.Key {
				case "name":
					name = a.Val
				case "value":
					value = a.Val
				}
			}
			if name != "" {
				form.Set(name, value)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, form)
		}
		if n.Type == html.ElementNode && n.Data == "form" && form != nil && form.Get("version") != "" {
			forms[form.Get("version")] = form
		}
	}
	walk(doc, nil)
	return forms
}

// The panel's own restore form is the journey a person takes: it must quote the
// write count the page was read at, so that an edit landing between reading the
// history and pressing Restore is refused rather than overwritten.
func TestThePanelsRestoreFormRefusesAnEditMadeSinceThePageWasRead(t *testing.T) {
	w, router, created, screen := contentHistoryFixture(t)
	req := httptest.NewRequest(http.MethodGet, "http://"+acmeHost+screen+"/"+created.ID.String(), nil)
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail = %d %s", rec.Code, rec.Body.String())
	}
	form, ok := restoreForms(t, rec.Body.String())["1"]
	if !ok {
		t.Fatalf("the detail page offers no restore form for version 1:\n%s", rec.Body.String())
	}
	if got := form.Get("expectedRevision"); got != "2" {
		t.Errorf("the panel's restore form quotes expectedRevision=%q, want the revision the page read, 2", got)
	}

	if code, body := call(t, router, acmeHost, http.MethodPatch, "/api/v1/content/contents/"+created.ID.String(),
		`{"body":"Someone else's text"}`); code != http.StatusOK {
		t.Fatalf("concurrent edit = %d %s", code, body)
	}
	before := w.versionsOf(created.ID)
	stale := contentRestoreForm(t, router, created, screen, form.Encode(), "en")
	if stale.Code != http.StatusPreconditionFailed {
		t.Errorf("the panel's restore after a concurrent edit = %d, want 412", stale.Code)
	}
	var body string
	if err := w.admin.QueryRowContext(t.Context(),
		"SELECT body FROM contents WHERE id = $1", created.ID).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(body) != "Someone else's text" {
		t.Errorf("the refused restore left the body %q, want the concurrent edit kept", body)
	}
	if after := w.versionsOf(created.ID); len(after) != len(before) {
		t.Errorf("the refused restore left %d versions, want %d", len(after), len(before))
	}
	if n := w.outbox(rest.RestoredEvent); n != 0 {
		t.Errorf("the refused restore emitted %d restored events, want none", n)
	}
}
