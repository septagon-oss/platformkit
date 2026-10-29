package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/health"
)

func TestReadinessMethodRefusalUsesTheNegotiatedErrorDocument(t *testing.T) {
	api, router := setupStatusFault(t, faultCase{})
	health.Register(api, nil)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+host+"/ready", nil)
	req.Header.Set("Accept", "text/html")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusMethodNotAllowed {
		t.Fatalf("unsupported readiness method answered %d, want 405", res.Code)
	}
	if got := res.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Errorf("readiness method refusal = %s: %s; want HTML", got, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "<html") {
		t.Error("readiness method refusal has no HTML document")
	}
}
