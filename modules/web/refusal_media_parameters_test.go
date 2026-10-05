package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRefusalNegotiationReadsQualityAfterMediaParameters(t *testing.T) {
	router, _, _ := site(t)
	for _, accept := range []string{
		"text/html;charset=utf-8;q=0, application/json",
		"text/html;charset=utf-8;q=0.2, application/json;q=0.9",
	} {
		t.Run(accept, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+host+"/no-such-page", nil)
			req.Header.Set("Accept", accept)
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			if res.Code != http.StatusNotFound {
				t.Fatalf("unknown page status = %d, want 404", res.Code)
			}
			if got := res.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/problem+json") {
				t.Errorf("Accept %q prefers JSON but refusal is %q", accept, got)
			}
		})
	}
}
