package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestTranslatableRecordShowsLocaleCompleteness(t *testing.T) {
	cfg, who, id := publishedTranslationPage(t, map[string]string{"title": "A nossa história", "body": "O parágrafo português."})
	status, html := do(t, cfg, who, http.MethodGet, acmeHost, "/app/content/contents/"+id, "")
	if status != http.StatusOK {
		t.Fatalf("record screen = %d", status)
	}
	if !strings.Contains(html, "Our story") {
		t.Fatal("record screen does not contain the created record")
	}
	if !strings.Contains(html, "100%") {
		t.Error("a record translated in every field has no 100% locale completeness badge")
	}
}
