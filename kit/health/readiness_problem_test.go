package health_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReadinessProblemBytesRemainStableForMonitors(t *testing.T) {
	h, _ := serve(t, check{name: "queue", err: errors.New("unavailable")})
	req := httptest.NewRequest(http.MethodGet, "http://10.0.0.7:8080/ready", nil)
	req.Header.Set("Accept", "application/json")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("readiness status = %d, want 503", res.Code)
	}
	if got := res.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("readiness media type = %q, want application/problem+json", got)
	}
	const before = `{"type":"about:blank","title":"Service Unavailable","status":503,"detail":"not ready: queue"}`
	if got := res.Body.String(); got != before {
		t.Errorf("the monitor's established problem bytes changed: got %q, want %q", got, before)
	}
}
