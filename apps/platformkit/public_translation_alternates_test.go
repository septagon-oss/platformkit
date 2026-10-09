package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestPublicSourcePageListsItsCompleteTranslations(t *testing.T) {
	cfg, _, _ := publishedTranslationPage(t, map[string]string{"title": "A nossa história", "body": "O parágrafo português."})
	for _, path := range []string{"/translated-story", "/translated-story?lang=en"} {
		status, html := getLanguage(t, cfg, acmeHost, path, "en")
		if status != http.StatusOK {
			t.Fatalf("GET %s = %d", path, status)
		}
		if !strings.Contains(html, "The original English paragraph.") {
			t.Fatalf("GET %s did not serve the source", path)
		}
		for _, tag := range []string{"en", "pt-PT", "x-default"} {
			if !strings.Contains(html, `hreflang="`+tag+`"`) {
				t.Errorf("GET %s omits the %s alternate of a fully translated page", path, tag)
			}
		}
	}
}
