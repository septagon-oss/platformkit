package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAClientAskingForAnUnknownPageGetsAProblemDocument(t *testing.T) {
	h, _, _ := site(t)
	req := httptest.NewRequest(http.MethodGet, "http://"+host+"/no-such-page", nil)
	req.Header.Set("Accept", "application/json")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("unknown page status = %d, want 404", res.Code)
	}
	body := res.Body.String()
	if got := res.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/problem+json") ||
		!strings.Contains(body, `"status":404`) || strings.Contains(body, "<html") {
		if len(body) > 220 {
			body = body[:220] + "…"
		}
		t.Errorf("a client asked for a problem document and got %q with body %s", got, body)
	}
}
