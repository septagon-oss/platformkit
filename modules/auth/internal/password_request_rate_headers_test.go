package internal_test

import (
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func TestPasswordRequestsReportTheLimitThatDecidesThem(t *testing.T) {
	router, _, _ := mount(t, auth.OIDC{})
	for attempt := range contracts.ResetRequests + 1 {
		got := call(t, router, http.MethodPost, "/api/v1/public/auth/password/forgot", `{"email":"unknown@acme.localhost"}`)
		want := http.StatusOK
		if attempt == contracts.ResetRequests {
			want = http.StatusTooManyRequests
		}
		if got.Code != want {
			t.Fatalf("attempt %d = %d %s, want %d", attempt+1, got.Code, got.Body.String(), want)
		}
		if attempt == 0 || attempt == contracts.ResetRequests {
			if got.Header().Get(httpx.RateLimitPolicyHeader) == "" || got.Header().Get(httpx.RateLimitHeader) == "" {
				t.Errorf("attempt %d status=%d: RateLimit-Policy=%q RateLimit=%q; the auth kit/limit policy must report both on allowed and refused replies", attempt+1, got.Code, got.Header().Get(httpx.RateLimitPolicyHeader), got.Header().Get(httpx.RateLimitHeader))
			}
		}
	}
}
