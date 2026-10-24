package health_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/septagon-oss/platformkit/kit/health"
)

// TestAProbeMountedWithNoFaultDoorAnswersTheDocumentAMonitorDiffs: Mux is the entry
// point of a worker served alone, which has no *httpx.API to borrow a door from. Its
// verdict, media type and bytes are the readiness case's — what a caller without a
// door is missing is the page a person could be shown, not the answer the deployment
// reads — and the verb gate is the mux's own either way, so a POST is refused in the
// same shape with the Allow header RFC 9110 owes it.
func TestAProbeMountedWithNoFaultDoorAnswersTheDocumentAMonitorDiffs(t *testing.T) {
	h := health.Mux(nil, []health.Check{check{name: "queue", err: errors.New("unavailable")}})
	res := probe(t, h, "10.0.0.7:8080", "/ready")
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("readiness status = %d, want 503", res.Code)
	}
	if got := res.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("readiness media type = %q, want application/problem+json", got)
	}
	const want = `{"type":"about:blank","title":"Service Unavailable","status":503,"detail":"not ready: queue"}`
	if got := res.Body.String(); got != want {
		t.Errorf("a probe with no fault door answered %q, want the document the readiness case pins (%q)", got, want)
	}

	post := httptest.NewRequest(http.MethodPost, "http://10.0.0.7:8080/ready", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, post)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /ready = %d, want 405 from a mux that answers its own verb gate", w.Code)
	}
	if got := w.Header().Get("Allow"); got != "GET, HEAD" {
		t.Errorf("the 405 offers Allow %q, want %q", got, "GET, HEAD")
	}
	if got := w.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("the 405 is labelled %q, want the one problem media type", got)
	}
}
