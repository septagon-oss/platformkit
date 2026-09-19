package main

// The adoption, at the level where it is actually true or false.
//
// kit/httpx can offer a failure renderer and ui/page can supply one, and neither of those
// facts means a person gets a page. This composes the real application — the same options
// function the binary's two entry points use, not a copy of them — and asks the running
// server what a browser is handed when a guard refuses it.

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// TestAWholeApplicationAnswersABrowserWithAPageAndAClientWithAValue. The two halves are
// one test because the defect is choosing the wrong one: a person who navigated and gets
// JSON, or an SDK that gets a page and fails to parse it.
func TestAWholeApplicationAnswersABrowserWithAPageAndAClientWithAValue(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)

	c := compose(cfg)
	options := appOptions(c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	signed := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	// A write that carries this site's session cookie and arrives from another
	// origin. The guard is right to refuse it; what the person is shown is the
	// part that was wrong.
	browser, contentType, body := crossSite(t, cfg, signed, acmeHost, tasksPath, `{"title":"from another site"}`)
	if browser != http.StatusForbidden {
		t.Fatalf("a cross-site write = %d, want the guard to refuse it: %s", browser, trimHTML(body))
	}
	if !strings.HasPrefix(contentType, "text/html") {
		t.Errorf("a browser navigation to a guard's refusal got %s, not a page: %s", contentType, trimHTML(body))
	}
	for _, want := range []string{
		"<title>Forbidden",      // the verdict, where a browser tab and a history entry read it
		"csrf:",                 // the reason, which here is actionable
		"Back to the workspace", // one way out
		"(request ",             // the reference, as a bare id a person can read aloud
		`href="/admin/assets/`,  // and the shell's own stylesheet, so it looks like the application
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the refusal page this application serves omits %q: %s", want, trimHTML(body))
		}
	}

	// The reference has to be the request's own identifier and not something the page
	// invented: the JSON body carries the same value as an instance URN, and a log line
	// is found by the id, not by the words around it.
	const marker = "(request "
	at := strings.Index(body, marker)
	if at < 0 {
		t.Fatalf("no reference on the page: %s", trimHTML(body))
	}
	id := body[at+len(marker):]
	if end := strings.IndexByte(id, ')'); end > 0 {
		id = id[:end]
	}
	if len(id) != 36 || strings.Count(id, "-") != 4 {
		t.Errorf("the reference is %q, which is not the request id a log line is found by", id)
	}
	if strings.Contains(body, "urn:request:") {
		t.Errorf("the page shows the raw URN; the person reading it wants the id, not the scheme")
	}

	// The same refusal, for a client that asked for a value: unchanged, because that
	// body is what SDKs, monitors and this repository's own suite parse.
	value, valueCT, valueBody := crossSiteAccept(t, cfg, signed, acmeHost, tasksPath, `{"title":"from another site"}`, "application/json")
	if value != http.StatusForbidden {
		t.Errorf("the verdict changed for a JSON client: %d", value)
	}
	if !strings.HasPrefix(valueCT, "application/problem+json") {
		t.Errorf("an API client got %s, want the problem document: %s", valueCT, valueBody)
	}
	if !strings.Contains(valueBody, `"detail":"csrf:`) {
		t.Errorf("the problem body is no longer the one clients read: %s", valueBody)
	}
}

// crossSite posts with a browser's own Accept header from a foreign origin.
func crossSite(t *testing.T, cfg config.Config, client *http.Client, host, path, body string) (int, string, string) {
	t.Helper()
	return crossSiteAccept(t, cfg, client, host, path, body, "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
}

func crossSiteAccept(t *testing.T, cfg config.Config, client *http.Client, host, path, body, accept string) (int, string, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://"+cfg.Server.Addr+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = host
	req.Header.Set("Content-Type", "application/json")
	// The header that makes it a cross-site write: this is what a browser sends when
	// a page on another site posts to us with a session cookie attached.
	req.Header.Set("Origin", "http://somewhere-else.test")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s at %s: %v", path, host, err)
	}
	defer res.Body.Close()
	out, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, res.Header.Get("Content-Type"), string(out)
}

// trimHTML keeps a failure readable: a refusal page is a whole document, and a whole one
// in a test log hides the assertion under it.
func trimHTML(body string) string {
	const keep = 700
	if len(body) > keep {
		return body[:keep] + "…"
	}
	return body
}
