package main

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestTenantLanguageChangesApplyToTheNextRequest(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, contentType, body string
		status                                int
	}{
		{"api", http.MethodPut, "/api/v1/tenant/languages", "application/json",
			`{"default":"pt-PT","supported":[],"expectedRevision":%s}`, http.StatusOK},
		{"page", http.MethodPost, "/app/tenant/languages", "application/x-www-form-urlencoded",
			"default=pt-PT&localeRevision=%s", http.StatusSeeOther},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, client := languageApp(t, `{"default":"en","supported":["pt-PT"]}`)
			code, _, declared, body := r7navigate(t, cfg, client, acmeHost, "/app", "en")
			if code != http.StatusOK || declared != "en" || pageLang(body) != "en" {
				t.Fatalf("initial dashboard = %d, Content-Language=%q, lang=%q", code, declared, pageLang(body))
			}
			code, held := do(t, cfg, client, http.MethodGet, acmeHost, "/api/v1/tenant/languages", "")
			revision := regexp.MustCompile(`"revision":\s*(\d+)`).FindStringSubmatch(held)
			if code != http.StatusOK || len(revision) != 2 {
				t.Fatalf("read language declaration = %d: %s", code, held)
			}
			req, err := http.NewRequestWithContext(t.Context(), tc.method, "http://"+cfg.Server.Addr+tc.path,
				strings.NewReader(fmt.Sprintf(tc.body, revision[1])))
			if err != nil {
				t.Fatal(err)
			}
			req.Host = acmeHost
			req.Header.Set("Content-Type", tc.contentType)
			req.Header.Set("Accept-Language", "en")
			posting := *client
			posting.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			response, err := posting.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(response.Body)
			response.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != tc.status {
				t.Fatalf("save languages = %d: %s", response.StatusCode, raw)
			}
			code, held = do(t, cfg, client, http.MethodGet, acmeHost, "/api/v1/tenant/languages", "")
			if code != http.StatusOK || !strings.Contains(held, `"default":"pt-PT"`) ||
				!strings.Contains(held, `"supported":[]`) {
				t.Fatalf("committed declaration = %d: %s", code, held)
			}
			code, _, declared, body = r7navigate(t, cfg, client, acmeHost, "/app", "en")
			if code != http.StatusOK {
				t.Fatalf("dashboard after language change = %d", code)
			}
			if declared != "pt-PT" || pageLang(body) != "pt-PT" {
				t.Errorf("the next dashboard still uses a removed language: Content-Language=%q, lang=%q; want pt-PT", declared, pageLang(body))
			}
		})
	}
}
