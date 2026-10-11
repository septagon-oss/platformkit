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
	// The word beside the number, so a translator reads which job is owed rather
	// than working it out from a share.
	if !strings.Contains(html, "Complete") {
		t.Error("a record whole in pt-PT wears a bare percentage where the switcher names the state")
	}
}

// The other end of the same strip: a language the record has no row in at all reads
// 0% and says Missing, because "0%" and "0% · Missing" are the same number and
// different sentences — the first is a record behind, the second is a record nobody
// has started, and a translator who cannot tell them apart opens the wrong one.
func TestAnUntranslatedRecordNamesItsMissingState(t *testing.T) {
	cfg, who, id := publishedTranslationPage(t, nil)
	status, html := do(t, cfg, who, http.MethodGet, acmeHost, "/app/content/contents/"+id, "")
	if status != http.StatusOK {
		t.Fatalf("record screen = %d", status)
	}
	if !strings.Contains(html, "0%") {
		t.Fatal("a record with no translation of it wears no 0% badge")
	}
	if !strings.Contains(html, "Missing") {
		t.Error("an untranslated record names no Missing state")
	}
	if strings.Contains(html, "Outdated") || strings.Contains(html, "Machine") {
		t.Error("a record with no row at all claims a translation it does not have")
	}
}
