package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRefusalNegotiationHonorsThePreferredRepresentationInEitherOrder(t *testing.T) {
	router, _, _ := site(t)
	for _, accept := range []string{
		"text/html;q=1, application/json;q=0.5",
		"application/json;q=0.5, text/html;q=1",
	} {
		t.Run(accept, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet,
				"http://"+host+"/no-such-page", nil)
			req.Header.Set("Accept", accept)
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			if res.Code != http.StatusNotFound {
				t.Fatalf("unknown page answered %d, want 404", res.Code)
			}
			if got := res.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
				t.Errorf("Accept %q prefers HTML but refusal is %q: %s", accept, got, res.Body.String())
			}
		})
	}
}
