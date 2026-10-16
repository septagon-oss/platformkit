package httpx

import (
	"net/http"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/idempotency"
)

func TestRecordedRefusalWaitsForEveryExhaustedWindowAndIgnoresOpenQuota(t *testing.T) {
	at := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	limits := []idempotency.Limit{
		{Name: "short-entries", Quota: 1, Remaining: 0, Window: 2 * time.Second, ResetsAt: at.Add(2 * time.Second)},
		{Name: "long-entries", Quota: 2, Remaining: 0, Window: 5 * time.Second, ResetsAt: at.Add(5 * time.Second)},
		{Name: "open-entries", Quota: 60, Remaining: 59, Window: time.Minute, ResetsAt: at.Add(time.Minute)},
	}
	const policy = `"short-entries";q=1;w=2,"long-entries";q=2;w=5,"open-entries";q=60;w=60`
	for _, tc := range []struct {
		name  string
		after time.Duration
		wait  string
		rate  string
	}{
		{
			name: "both exhausted windows are open", wait: "5",
			rate: `"short-entries";r=0;t=2,"long-entries";r=0;t=5,"open-entries";r=59;t=60`,
		},
		{
			name: "only the longer exhausted window is open", after: 3 * time.Second, wait: "2",
			rate: `"short-entries";r=0;t=0,"long-entries";r=0;t=2,"open-entries";r=59;t=57`,
		},
		{
			name: "all exhausted windows have closed", after: 6 * time.Second, wait: "1",
			rate: `"short-entries";r=0;t=0,"long-entries";r=0;t=0,"open-entries";r=59;t=54`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := make(http.Header)
			h.Set(RateLimitPolicyHeader, policy)
			h.Set(RateLimitHeader, `"open-entries";r=58;t=60`)
			sayRecorded(h, limits, true, at.Add(tc.after))
			if got := h.Get(RetryAfterHeader); got != tc.wait {
				t.Errorf("Retry-After = %q; want %q from exhausted windows only", got, tc.wait)
			}
			if got := h.Values(RateLimitHeader); len(got) != 1 || got[0] != tc.rate {
				t.Errorf("RateLimit = %q; want each stored reading once: %q", got, tc.rate)
			}
			if got := h.Values(RateLimitPolicyHeader); len(got) != 1 || got[0] != policy {
				t.Errorf("RateLimit-Policy = %q; want the whole deciding policy %q", got, policy)
			}
		})
	}
}
