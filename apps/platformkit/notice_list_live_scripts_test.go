package main

import (
	"net/http"
	"regexp"
	"testing"

	"github.com/septagon-oss/platformkit/ui"
)

// TestTheComposedNoticeListLoadsWhatItsLiveRegionRunsOn: the in-app list this
// application composes is drawn as a live region, and a live region runs on three
// files the browser must be given — htmx, which the pinned extension registers
// itself with the moment it runs; the extension; and the controller file that
// turns a frame into one refresh. A document that carries the extension without
// htmx throws on load and never opens its connection, so a notice reaches the list
// only when the person reloads.
func TestTheComposedNoticeListLoadsWhatItsLiveRegionRunsOn(t *testing.T) {
	cfg, admin := started(t)
	status, _, page := askNavigate(t, cfg, admin, acmeHost, "/app/notification/notifications")
	if status != http.StatusOK {
		t.Fatalf("GET /app/notification/notifications = %d, want the list: %s", status, firstLineOf(page))
	}
	if !regexp.MustCompile(`data-controller="live"`).MatchString(page) {
		t.Fatalf("the composed list is not drawn as a live region:\n%s", firstLineOf(page))
	}
	at := func(file string) int {
		loc := regexp.MustCompile(`<script[^>]*src="[^"]*/js/` + regexp.QuoteMeta(file) + `[^"]*"`).FindStringIndex(page)
		if loc == nil {
			return -1
		}
		return loc[0]
	}
	extension, htmx, controllers := at(ui.SSEExtension), at("htmx.min.js"), at("components.js")
	if extension < 0 {
		t.Fatalf("the live list does not load the pinned extension %s", ui.SSEExtension)
	}
	if htmx < 0 {
		t.Errorf("the live list loads %s but not htmx.min.js, which the extension registers itself with", ui.SSEExtension)
	} else if htmx > extension {
		t.Errorf("htmx.min.js is loaded after %s, which needs it when it runs", ui.SSEExtension)
	}
	if controllers < 0 {
		t.Errorf("the live list does not load components.js, the file whose live controller turns a frame into a refresh")
	}
}
