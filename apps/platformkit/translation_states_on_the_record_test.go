package main

// The brief's locale switcher names a state, not only a share: Missing,
// Outdated, Machine, Complete. A bare percentage cannot tell a translator the
// one thing the staleness rule exists to say — that a number below 100 means
// the source moved under a translation somebody already reviewed, not that a
// field was never translated — and journey 2's badge reads "Outdated", in
// words. The record screen is where the delivery already answers the language
// question, so it is where the state belongs.
//
// Reachability is the record's own title and the strip's percentage, which the
// delivered behaviour prints either way; only the state word is the pin.

import (
	"net/http"
	"strings"
	"testing"
)

func TestARecordNamesTheStateOfAnOutdatedTranslation(t *testing.T) {
	cfg, who, id := publishedTranslationPage(t,
		map[string]string{"title": "A nossa história", "body": "O parágrafo português."})
	code, body := do(t, cfg, who, http.MethodPatch, acmeHost, contentPath+"/"+id,
		`{"body":"A different English paragraph."}`)
	if code != http.StatusOK {
		t.Fatalf("PATCH the source = %d %s", code, body)
	}
	status, screen := do(t, cfg, who, http.MethodGet, acmeHost, "/app/content/contents/"+id, "")
	if status != http.StatusOK || !strings.Contains(screen, "Our story") {
		t.Fatalf("record screen = %d", status)
	}
	if !strings.Contains(screen, "50%") {
		t.Fatalf("precondition: one reviewed field of two reads 50%% on the strip")
	}
	if !strings.Contains(screen, "Outdated") {
		t.Error("a record whose translation went stale names no Outdated state; the strip wears a bare percentage where the brief's switcher names Missing, Outdated, Machine and Complete")
	}
}
