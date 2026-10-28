package httpx_test

// A refusal of an anonymous write answers two figures out of one reading of one
// countdown: what is left of the window, which `RateLimit` states as it is, and the
// wait the caller is asked to sit out, which `Retry-After` states with a floor of a
// whole second. `kit/limit` clamps the countdown it reports at zero rather than below
// it, so a refusal whose row closed while the statement waited on another writer hands
// exactly the one reading on which the two figures differ — and the tree held that
// floor nowhere: rebuilding this door to ask `Retry-After: 0` left the whole package
// green. A shut door that asks no time at all asks to be retried this instant, from
// the one limit whose job is to make trying now useless, so it is refused here.
//
// The other rows pin the same answer against a countdown that still has seconds in it:
// stated up, never down, and never from a constant. A rounding of a refusal's wait
// down is a caller that retries early and is refused again, which is the traffic this
// response exists to keep out.

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

func TestAShutAnonymousWriteDoorNeverAsksToBeRetriedAtThisInstant(t *testing.T) {
	// publicWritePolicyName is unexported and this case is about the door, so the
	// policy the kernel names on the wire is spelled here as the caller reads it.
	const policy = `"public-writes";r=0;t=`
	for _, one := range []struct {
		name    string
		counter answeredLimit
		wait    string
		rate    string
	}{
		{
			name:    "the window closed as the counter read it",
			counter: answeredLimit{},
			wait:    "1",
			rate:    policy + "0",
		},
		{
			name:    "the window has half a second left in it",
			counter: answeredLimit{retry: 500 * time.Millisecond},
			wait:    "1",
			rate:    policy + "1",
		},
		{
			name:    "the window has two and a half seconds left in it",
			counter: answeredLimit{retry: 2500 * time.Millisecond},
			wait:    "3",
			rate:    policy + "3",
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			router, door, reached := writeDoor(t, one.counter)
			res := knock(t, router, door)
			if res.Code != http.StatusTooManyRequests || reached.Load() != 0 {
				t.Fatalf("an anonymous write past its limit = %d after %d reached the handler, want 429 and none: %s",
					res.Code, reached.Load(), res.Body)
			}
			got := res.Header().Get(httpx.RetryAfterHeader)
			if got != one.wait {
				t.Errorf("Retry-After = %q, want the %q this door's countdown states: a wait stated as none asks the caller to retry now", got, one.wait)
			}
			if line := res.Header().Get(httpx.RateLimitHeader); line != one.rate {
				t.Errorf("RateLimit = %s, want %s: the wait asked and the remainder left are two figures, and differ at the closed-window reading alone", line, one.rate)
			}
			if !strings.Contains(res.Body.String(), httpx.CodeLimitExhausted) {
				t.Errorf("the refusal carries no %s for the caller to read: %s", httpx.CodeLimitExhausted, res.Body)
			}
		})
	}
}
