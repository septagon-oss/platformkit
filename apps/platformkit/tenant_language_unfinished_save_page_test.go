package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// TestAnUnfinishedLanguageSaveShowsItsPageInTheRequestsLanguage submits the tenant's
// Settings → Languages form the way a browser does — asking for a page, in Portuguese
// — while the shared store refuses the invalidation once. The committed declaration
// stands, the person is shown a 503 page rather than sent on to a success, that page
// speaks the request's language, and the same form submitted again finishes the change.
func TestAnUnfinishedLanguageSaveShowsItsPageInTheRequestsLanguage(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	cfg.Cache = config.Cache{Adapter: "valkey", App: "tenant-language-unfinished-page", URL: "valkey://127.0.0.1:6379"}
	backend := cache.MemoryBackend()
	t.Cleanup(func() { _ = backend.Close() })
	store := &storeThatRefusesTheFirstClose{Cache: cache.MemoryNamed(cfg.Cache.App, backend)}
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	options.Caches = app.Caches{Valkey: func(context.Context, config.Cache) (cache.Cache, error) { return store, nil }}
	start(t, cfg, c.modules, options)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	code, held := do(t, cfg, admin, http.MethodGet, acmeHost, "/api/v1/tenant/languages", "")
	if code != http.StatusOK {
		t.Fatalf("read the current declaration = %d: %s", code, held)
	}
	var current struct {
		Default   string   `json:"default"`
		Supported []string `json:"supported"`
		Revision  *int64   `json:"revision"`
	}
	if err := json.Unmarshal([]byte(held), &current); err != nil || current.Revision == nil {
		t.Fatalf("read the declaration revision: %v: %s", err, held)
	}
	if !speaks(current.Default, current.Supported, "pt-PT") {
		t.Fatalf("the fixture tenant must already speak pt-PT for the request to ask for it: %s", held)
	}
	revision := fmt.Sprint(*current.Revision)
	if code, _, _, _ := r7navigate(t, cfg, admin, acmeHost, "/app", "en"); code != http.StatusOK {
		t.Fatalf("warm the tenant's dashboard = %d", code)
	}

	store.refused.Store(true)
	code, header, body := submitLanguagesPage(t, cfg, admin, revision)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("a browser save the store could not finish = %d (Location %q), want 503: %s",
			code, header.Get("Location"), firstLineOf(body))
	}
	if got := pageLang(body); got != "pt-PT" {
		t.Errorf("the unfinished save's page is in lang=%q, want the request's pt-PT: %s", got, firstLineOf(body))
	}
	if code, held = do(t, cfg, admin, http.MethodGet, acmeHost, "/api/v1/tenant/languages", ""); code != http.StatusOK || !strings.Contains(held, `"default":"pt-PT"`) {
		t.Fatalf("the committed declaration was lost = %d: %s", code, held)
	}

	code, header, body = submitLanguagesPage(t, cfg, admin, revision)
	if code != http.StatusSeeOther {
		t.Fatalf("the same form submitted after the store recovers = %d, want 303: %s", code, firstLineOf(body))
	}
	code, _, declared, page := r7navigate(t, cfg, admin, acmeHost, "/app", "de")
	if code != http.StatusOK || declared != "pt-PT" || pageLang(page) != "pt-PT" {
		t.Errorf("the resubmitted form did not finish the change: status=%d header=%q lang=%q", code, declared, pageLang(page))
	}
}

// speaks reports whether a declaration offers lang, as its default or beside it.
func speaks(def string, supported []string, lang string) bool {
	return def == lang || slices.Contains(supported, lang)
}

// submitLanguagesPage posts the languages form as a browser does: it asks for a page,
// ranks Portuguese first and follows no redirect, so where the save lands is read.
func submitLanguagesPage(t *testing.T, cfg config.Config, client *http.Client, revision string) (int, http.Header, string) {
	t.Helper()
	noRedirect := *client
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	form := url.Values{"default": {"pt-PT"}, "supported": {"en"}, "localeRevision": {revision}}
	req, err := http.NewRequest(http.MethodPost, "http://"+cfg.Server.Addr+"/app/tenant/languages", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Host = acmeHost
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "pt-PT")
	req.Header.Set("Referer", "http://"+acmeHost+"/app/tenant/languages")
	res, err := noRedirect.Do(req)
	if err != nil {
		t.Fatalf("POST /app/tenant/languages: %v", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read the answer: %v", err)
	}
	return res.StatusCode, res.Header, string(body)
}
