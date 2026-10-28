package httpx_test

// The kernel counts writes at two doors out of one rendering: an anonymous write at
// the public address, and a write whose route declares its own allowance on the
// signed-in surface. Both ask the same helper for the wait they state, and that
// helper's floor — never below one second — is what stops a refusal inviting back
// the traffic it just paid to turn away. The anonymous door has a case that asks the
// floor at the one reading where the countdown ran out; this door, the one a person's
// own submission passes, had a case that asked a wait of three seconds and never
// asked that reading, so a build which states no time here alone refused nothing.
//
// kit/limit makes the reading reachable: it clamps the countdown at zero
// (max(left, 0)) and dates its row by the transaction's now() while measuring the
// countdown by the statement's clock_timestamp(), so a window that closed while its
// statement waited on another writer is refused with a remainder of exactly zero.

import (
	"net/http"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

// TestTheSpentCountedDoorNeverAsksToBeRetriedAtThisInstant asks a route that
// declares its own write allowance the wait it states once that window is spent. The
// seconds never fall below one, and they round up rather than down out of a countdown
// that still has seconds in it: a wait stated as none, or as less than the window
// actually left, is a caller that retries early and is refused again. The refused
// write reaches the handler neither time.
func TestTheSpentCountedDoorNeverAsksToBeRetriedAtThisInstant(t *testing.T) {
	for _, one := range []struct {
		name string
		left time.Duration
		wait string
	}{
		{name: "the window closed as the counter read it", left: 0, wait: "1"},
		{name: "the window has two and a half seconds left in it", left: 2500 * time.Millisecond, wait: "3"},
	} {
		t.Run(one.name, func(t *testing.T) {
			counter := &recordedLimit{ok: false, retry: one.left}
			router, door, reached, _ := countedDoor(t, counter)
			res := submit(t, router, door)
			if res.Code != http.StatusTooManyRequests || reached.Load() != 0 {
				t.Fatalf("a counted write past its window = %d with the handler reached %d times, want 429 and never: %s",
					res.Code, reached.Load(), res.Body)
			}
			if got := res.Header().Get(httpx.RetryAfterHeader); got != one.wait {
				t.Errorf("Retry-After = %q, want %q: this door states its own countdown rounded up to a whole second and never below one",
					got, one.wait)
			}
		})
	}
}
