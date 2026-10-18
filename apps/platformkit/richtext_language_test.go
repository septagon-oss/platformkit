package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

func TestRichTextHelpSpeaksTheRequestLanguage(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	opts := appOptions(cfg, c, app.All)
	opts.Transport = memory.New()
	opts.Log = quiet()
	start(t, cfg, c.modules, opts)
	who := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	code, body := do(t, cfg, who, http.MethodGet, acmeHost,
		"/app/content/contents/new?lang=pt-PT", "")
	if code != http.StatusOK || !strings.Contains(body, `lang="pt-PT"`) ||
		!strings.Contains(body, `name="body"`) || !strings.Contains(body, "<details") {
		t.Fatalf("Portuguese rich-text form was not reached: %d %s", code, body)
	}
	if strings.Contains(body, "Formatting help") || strings.Contains(body, "Upload the file first") {
		t.Fatal("Portuguese form shows English rich-text help")
	}
}
