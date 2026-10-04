package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

func TestInvalidVerificationLinkSpeaksTheRequestedTenantLanguage(t *testing.T) {
	path, cfg := configure(t)
	path, cfg = keepMailInTheProcess(t, path, cfg)
	install(t, path) // This tenant declares both English and Portuguese.
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	status, contentType, language, body := r7navigate(t, cfg, &http.Client{}, acmeHost,
		"/auth/verify-email?token=invalid", "pt-PT")
	if status != http.StatusUnauthorized || !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("invalid link answered %d %s, want a 401 HTML refusal", status, contentType)
	}
	if language != "pt-PT" || !strings.Contains(body, `lang="pt-PT"`) {
		t.Errorf("Portuguese request received Content-Language=%q; want a Portuguese refusal", language)
	}
	if strings.Contains(body, "that verification link is invalid or has expired; request another link") {
		t.Error("the refusal's actionable sentence is literal English for a Portuguese reader")
	}
}
