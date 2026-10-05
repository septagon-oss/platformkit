package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestUnreadableTenantLanguageFormKeepsTheRequestsLanguage(t *testing.T) {
	cfg, client := languageApp(t, `{"default":"pt-PT","supported":["en"]}`)
	for _, body := range []string{"default=%ZZ", "default=en&localeRevision=invalid", "default=en"} {
		t.Run(body, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, "http://"+cfg.Server.Addr+"/app/tenant/languages", strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			req.Host = acmeHost
			req.Header.Set("Accept", "text/html")
			req.Header.Set("Accept-Language", "pt-PT")
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			response, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			raw, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("invalid form status=%d, want 422", response.StatusCode)
			}
			page := string(raw)
			if pageLang(page) != "pt-PT" {
				t.Errorf("Portuguese tenant form refusal declares lang=%q, want pt-PT", pageLang(page))
			}
			if strings.Contains(page, "this form could not be read") {
				t.Error("Portuguese tenant form refusal prints English: this form could not be read")
			}
		})
	}
}
