package page_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestALiveRefreshCannotReadTheRegionWithoutItsSession(t *testing.T) {
	router, inbox, _ := livePages(t)
	out := openWorkspace(t, router, inbox+"?page=2", htmxFetch())
	if !strings.Contains(out, "<p>one notice</p>") || strings.Contains(out, "<!doctype") {
		t.Fatalf("the signed-in refresh did not receive its region: %s", firstLine(out))
	}
	for _, headers := range []map[string]string{
		nil,
		htmxFetch(),
		{"HX-Request": "true", "HX-Boosted": "true"},
		{"HX-Request": "true", "HX-History-Restore-Request": "true"},
	} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet,
			"http://acme.localhost"+inbox+"?page=2", nil)
		req.Header.Set("Accept", "application/json")
		for name, value := range headers {
			req.Header.Set(name, value)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("anonymous fetch with headers %v answered %d: %s",
				headers, rec.Code, firstLine(rec.Body.String()))
		}
		for _, private := range []string{"one notice", `data-controller="live"`, "sse-connect="} {
			if strings.Contains(rec.Body.String(), private) {
				t.Errorf("anonymous fetch with headers %v exposed %q", headers, private)
			}
		}
	}
}
