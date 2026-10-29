package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

// TestSessionCookieIsFoundOnlyWhereARequestPresentsOne pins that skipping the name
// search for a request with no Cookie header changes no answer: the current name
// and the legacy name are still read, and an absent header finds nothing.
func TestSessionCookieIsFoundOnlyWhereARequestPresentsOne(t *testing.T) {
	bare := httptest.NewRequest(http.MethodGet, "/", nil)
	if c, ok := httpx.SessionCookieOf(bare); ok || c != nil {
		t.Fatalf("a request with no Cookie header found a session cookie: %v", c)
	}
	for _, name := range []string{httpx.SessionCookie, httpx.CookieName(httpx.SessionCookie, true), "platformkit_session"} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(&http.Cookie{Name: name, Value: "v"})
		if c, ok := httpx.SessionCookieOf(r); !ok || c.Value != "v" {
			t.Errorf("cookie %q presented but not found", name)
		}
	}
}
