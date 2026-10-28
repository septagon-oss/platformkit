package httpx

import (
	"net/http"
	"testing"
	"time"
)

// A refusal the module counted states the seconds its counter says are left of its
// window and asks the caller to sit out the same seconds, rounded up, and the ask keeps
// a floor of one second: `t` of zero is the honest reading of a window that closed as it
// was looked at, while `Retry-After` of zero is an instruction to try again this instant,
// which is what the counter exists to prevent. Both figures are rendered from one reading
// of one remainder, so a remainder under a whole second is the one case where the two
// figures of one answer legitimately differ — and the only case where the floor is doing
// any work. Rendered from a reading of the caller's choosing instead, both figures still
// fall and still agree at every whole second, so a case that reads them at one instant of
// a long window cannot see the floor missing.
func TestARefusedCounterNeverAsksToBeRetriedBeforeItsWindowCloses(t *testing.T) {
	const policy = "sign-in-attempts"
	for _, tc := range []struct {
		name string
		left time.Duration
		t    string
		wait string
	}{
		{name: "a window nine minutes from its close", left: 900 * time.Second, t: "900", wait: "900"},
		{name: "one whole second left", left: time.Second, t: "1", wait: "1"},
		{name: "a remainder under one second", left: 400 * time.Millisecond, t: "1", wait: "1"},
		{name: "the window closed as we looked at it", left: 0, t: "0", wait: "1"},
		{name: "the window closed before we looked at it", left: -3 * time.Second, t: "0", wait: "1"},
	} {
		h := make(http.Header)
		sayCountedOnto(h, measured{
			policy:    limitPolicy{name: policy, quota: 10, window: 15 * time.Minute},
			remaining: 0, left: tc.left,
		}, true)
		if got, want := h.Get(RetryAfterHeader), tc.wait; got != want {
			t.Errorf("%s: Retry-After = %q, want %q: a shut door whose window has no whole second left still asks a wait, not an instant",
				tc.name, got, want)
		}
		if got, want := h.Get(RateLimitHeader), `"`+policy+`";r=0;t=`+tc.t; got != want {
			t.Errorf("%s: RateLimit = %q, want %q: the seconds a shut door states are its own remainder, floored at zero",
				tc.name, got, want)
		}
	}
}
