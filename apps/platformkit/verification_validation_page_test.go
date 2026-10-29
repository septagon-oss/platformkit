package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

func TestVerificationValidationAnswersABrowserWithAPage(t *testing.T) {
	path, cfg := configure(t)
	path, cfg = keepMailInTheProcess(t, path, cfg)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	status, contentType, body := askNavigate(t, cfg, &http.Client{}, acmeHost,
		"/auth/verify-email?token="+strings.Repeat("x", 129))
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("invalid credential length answered %d, want 422", status)
	}
	if !strings.HasPrefix(contentType, "text/html") {
		t.Errorf("browser validation refusal = %s: %s; want an HTML error page", contentType, body)
	}
	if !strings.Contains(body, "Request reference") {
		t.Error("validation refusal has no labelled request reference for the person")
	}
}
