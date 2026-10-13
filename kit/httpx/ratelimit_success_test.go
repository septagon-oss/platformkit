package httpx_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/limit"
)

func TestAllowedWriteReportsItsRemainingRateLimit(t *testing.T) {
	_, router, _ := keyedSetup(t, nil, limit.Memory())
	got := keyedPost(t, router, http.MethodPost, "/api/v1/public/probe/writes", "", "{}")
	if got.Code != http.StatusCreated {
		t.Fatalf("write = %d %s", got.Code, got.Body.String())
	}
	if got.Header().Get("RateLimit-Policy") == "" {
		t.Error("allowed write has no RateLimit-Policy")
	}
	if rate := got.Header().Get("RateLimit"); !strings.Contains(rate, `"public-writes";r=59;t=`) {
		t.Errorf("allowed write RateLimit = %q, want remaining quota 59 and reset time", rate)
	}
}
