package main

import (
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestRecoveryPagesTranslateTheirWorkspaceLink(t *testing.T) {
	cfg, client := languageApp(t, `{"default":"en","supported":["pt-PT"]}`)
	for _, tc := range []struct {
		path   string
		status int
	}{
		{"/app/access-request/sent", http.StatusOK},
		{"/app/does-not-exist", http.StatusNotFound},
	} {
		t.Run(tc.path, func(t *testing.T) {
			code, ctype, declared, body := r7navigate(t, cfg, client, acmeHost, tc.path, "pt-PT")
			if code != tc.status || !strings.HasPrefix(ctype, "text/html") || declared != "pt-PT" || pageLang(body) != "pt-PT" {
				t.Fatalf("recovery page = %d %s, Content-Language=%q, lang=%q; want Portuguese HTML %d", code, ctype, declared, pageLang(body), tc.status)
			}
			label := ""
			for _, link := range regexp.MustCompile(`(?s)<a\b[^>]*href="/app"[^>]*>(.*?)</a>`).FindAllStringSubmatch(body, -1) {
				label = strings.TrimSpace(html.UnescapeString(regexp.MustCompile(`<[^>]*>`).ReplaceAllString(link[1], "")))
			}
			if label == "" || label == "Back to the workspace" {
				t.Errorf("Portuguese recovery page must offer a translated workspace link, got %q", label)
			}
		})
	}
}
