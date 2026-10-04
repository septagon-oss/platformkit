package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"golang.org/x/net/html"
)

func TestStaleAccountLanguageFormCannotOverwriteANewerChoice(t *testing.T) {
	cfg, client := languageApp(t, `{"default":"en","supported":["pt-PT"]}`)
	// Capture the actual form, including any revision supplied by the renderer.
	root, err := html.Parse(strings.NewReader(dashboard(t, cfg, client, "en")))
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{}
	action := ""
	var walk func(*html.Node, bool)
	walk = func(node *html.Node, account bool) {
		attrs := map[string]string{}
		for _, a := range node.Attr {
			attrs[a.Key] = a.Val
		}
		if node.Data == "form" {
			_, account = attrs["data-account-language"]
			if account {
				action = attrs["action"]
			}
		}
		if account && node.Data == "input" && attrs["name"] != "" {
			form.Set(attrs["name"], attrs["value"])
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child, account)
		}
	}
	walk(root, false)
	if action == "" {
		t.Fatal("the signed-in page has no account language form")
	}
	// Another tab changes the choice after the captured form was drawn.
	code, body := do(t, cfg, client, http.MethodPut, acmeHost, "/api/v1/user/me/language",
		`{"language":"pt-PT","expectedRevision":0}`)
	if code != http.StatusOK {
		t.Fatalf("newer choice = %d: %s", code, body)
	}
	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	count := func() int {
		t.Helper()
		var n int
		if err := owner.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox WHERE name = 'user.language_set'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := count()
	form.Set("locale", "en")
	req, err := http.NewRequest(http.MethodPost, "http://"+cfg.Server.Addr+action, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = acmeHost
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "http://"+acmeHost+"/app")
	noRedirect := *client
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	code, body = do(t, cfg, client, http.MethodGet, acmeHost, "/api/v1/user/me/language", "")
	var held struct {
		Language string `json:"language"`
		Revision int64  `json:"revision"`
	}
	if code != http.StatusOK || json.Unmarshal([]byte(body), &held) != nil {
		t.Fatalf("read choice = %d: %s", code, body)
	}
	if held.Language != "pt-PT" || held.Revision != 1 || count() != before {
		t.Errorf("stale form returned %d and left language=%q revision=%d events=%d (before %d); want pt-PT, revision 1 and no event",
			response.StatusCode, held.Language, held.Revision, count(), before)
	}
}
