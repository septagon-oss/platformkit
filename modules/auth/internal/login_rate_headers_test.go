package internal_test

import (
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func TestPasswordLockoutStatesTheLimitThatRefusedIt(t *testing.T) {
	router, _, _ := mount(t, auth.OIDC{})
	for attempt := range contracts.MaxAttempts + 1 {
		got := call(t, router, http.MethodPost, "/api/v1/auth/login", `{"email":"unknown@acme.localhost","password":"wrong-password"}`)
		if attempt < contracts.MaxAttempts {
			if got.Code != http.StatusUnauthorized {
				t.Fatalf("attempt %d = %d %s; want 401 before lockout", attempt+1, got.Code, got.Body.String())
			}
			continue
		}
		if got.Code != http.StatusTooManyRequests {
			t.Fatalf("exhausted password counter = %d %s; want 429", got.Code, got.Body.String())
		}
		for _, name := range []string{httpx.RateLimitPolicyHeader, httpx.RateLimitHeader, httpx.RetryAfterHeader} {
			if got.Header().Get(name) == "" {
				t.Errorf("password lockout answered %d with no %s", got.Code, name)
			}
		}
	}
}
