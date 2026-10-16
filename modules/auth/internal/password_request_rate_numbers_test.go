package internal_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// TestPasswordRequestsStateTheCounterThatDecidedThem reads the numbers, where
// TestPasswordRequestsReportTheLimitThatDecidesThem asks only that two fields are
// not empty. Both fields are the deciding counter's or neither is worth reading:
// the name has to be the cap the route spent, the quota the one ResetRequests is,
// the quota left has to fall one per attempt from the attempt that spent the first,
// and the window has to be the one the counter is measuring, not a number this
// module typed beside a name.
func TestPasswordRequestsStateTheCounterThatDecidedThem(t *testing.T) {
	router, _, _ := mount(t, auth.OIDC{})
	policy := `"` + contracts.AccountRequestsPolicyName + `";q=` + strconv.Itoa(contracts.ResetRequests) + ";w=" + strconv.Itoa(int(contracts.AttemptWindow.Seconds()))
	for attempt := range contracts.ResetRequests + 1 {
		got := call(t, router, http.MethodPost, "/api/v1/public/auth/password/forgot", `{"email":"unknown@acme.localhost"}`)
		if items := got.Header().Values(httpx.RateLimitPolicyHeader); len(items) != 1 || items[0] != policy {
			t.Errorf("attempt %d: RateLimit-Policy=%q, want this one item %q", attempt+1, items, policy)
		}
		rate := got.Header().Get(httpx.RateLimitHeader)
		name := `"` + contracts.AccountRequestsPolicyName + `"`
		left, ok := strings.CutPrefix(rate, name+";r=")
		if !ok {
			t.Fatalf("attempt %d: RateLimit=%q, want it to name %s", attempt+1, rate, name)
		}
		// `max`, because the counter stops at zero: the event over the quota is
		// refused and reports nothing left rather than a debt.
		want := max(contracts.ResetRequests-attempt-1, 0)
		after, ok := strings.CutPrefix(left, strconv.Itoa(want)+";t=")
		if !ok {
			t.Fatalf("attempt %d: RateLimit=%q, want r=%d, the quota left by this attempt alone", attempt+1, rate, want)
		}
		// And nothing after the number: this answer spent one counter, so a second item
		// here would be the kernel's anonymous-write limit stating a quota this route
		// was never counted against.
		if seconds, err := strconv.Atoi(after); err != nil || seconds <= 0 || seconds > int(contracts.AttemptWindow.Seconds()) {
			t.Errorf("attempt %d: RateLimit=%q, want %s;r=%d;t=<seconds of the %s window> and no other item",
				attempt+1, rate, name, want, contracts.AttemptWindow)
		}
		if refused := attempt == contracts.ResetRequests; refused {
			if wait := got.Header().Get(httpx.RetryAfterHeader); wait == "" {
				t.Errorf("the refused attempt answered 429 with no Retry-After")
			} else if seconds, err := strconv.Atoi(wait); err != nil || seconds <= 0 || seconds > int(contracts.AttemptWindow.Seconds()) {
				t.Errorf("Retry-After=%q, a wait inside the window the counter reported", wait)
			}
		}
	}
}
