package main

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

func TestUnreadableVerificationFormSpeaksTheRequestedTenantLanguage(t *testing.T) {
	path, cfg := configure(t)
	path, cfg = keepMailInTheProcess(t, path, cfg)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		"http://"+cfg.Server.Addr+"/auth/verify-email", strings.NewReader("token=%zz"))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = acmeHost
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", "pt-PT")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("unreadable form status = %d, want 422", res.StatusCode)
	}
	if !strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("unreadable form returned %s, want HTML", res.Header.Get("Content-Type"))
	}
	if language := res.Header.Get("Content-Language"); language != "pt-PT" {
		t.Errorf("unreadable verification form Content-Language = %q, want pt-PT", language)
	}
	if strings.Contains(string(body), "this form could not be read") {
		t.Error("Portuguese reader received the English refusal: this form could not be read")
	}
}
