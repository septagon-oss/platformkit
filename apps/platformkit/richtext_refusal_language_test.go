package main

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

func TestRichTextRefusalSpeaksTheRequestLanguage(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	opts := appOptions(cfg, c, app.All)
	opts.Transport = memory.New()
	opts.Log = quiet()
	start(t, cfg, c.modules, opts)
	who := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	formPath := "/app/content/contents/new?lang=pt-PT"
	code, form := do(t, cfg, who, http.MethodGet, acmeHost, formPath, "")
	if code != http.StatusOK || !strings.Contains(form, `lang="pt-PT"`) ||
		!strings.Contains(form, "Ajuda de formatação") {
		t.Fatalf("Portuguese form not reached: %d %s", code, form)
	}

	bad := "<script>alert(1)</script>"
	values := url.Values{"slug": {"portuguese-refusal"}, "title": {"Portuguese refusal"}, "body": {bad}}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		"http://"+cfg.Server.Addr+"/app/content/contents?lang=pt-PT", strings.NewReader(values.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = acmeHost
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html")
	res, err := who.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	page := string(body)
	if res.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(page, `lang="pt-PT"`) ||
		!strings.Contains(page, `name="body"`) {
		t.Fatalf("Portuguese refused form not reached: status %d, language or body field absent", res.StatusCode)
	}
	for _, sentence := range []string{"Write Markdown instead of HTML.", "Unsupported raw HTML"} {
		if strings.Contains(page, sentence) {
			t.Fatalf("Portuguese refusal contains English instruction %q", sentence)
		}
	}
}
