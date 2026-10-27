package internal_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// A lockout window is a span of wall time that opened when the first failed attempt
// arrived, so the countdown a shut door states is that span's remainder and can only
// live between now and the window's own length. A reading compared against the number
// in the same answer — the wait against the seconds left, the ceiling against the
// policy's quota — cannot see both numbers move together: a door that reported the
// elapsed part of its window, a stale remainder kept beside a reset counter, or one
// that halved what its counter gave it would state two agreeing numbers and an answer
// that is wrong by minutes. So this asks the two password doors from one machine about
// one address until the lockout is out, which opens the window as it goes, and compares
// the first shut answer's countdown with the clock the asks themselves ran against: the
// seconds left are the window with only the elapsed span taken off it, never more than
// the window and never the elapsed span itself.
func TestAShutSignInDoorCountsDownFromTheWindowItsOwnAsksOpened(t *testing.T) {
	const (
		address = "the.window.of.the.first.failure@acme.localhost"
		machine = "203.0.113.254"
		// slack is how far the countdown may sit below the window's length on top of
		// what the asks themselves took: a test binary that stops for a garbage
		// collection or a busy database must not answer this with a failure, while a
		// remainder no longer than half the window still has to.
		slack = 120 * time.Second
	)
	window := contracts.AttemptWindow
	asked := []struct{ name, path, body string }{
		{"password", "/api/v1/auth/login", `{"email":"` + address + `","password":"no password anyone here was given"}`},
		{"second half", "/api/v1/auth/challenge/verify", `{"email":"` + address + `","code":"000000"}`},
	}
	router, _, _ := mount(t, auth.OIDC{})
	opened := time.Now()
	for ask := 1; ask <= 2*contracts.MaxAttempts+1; ask++ {
		which := asked[(ask-1)%len(asked)]
		res := call(t, router, http.MethodPost, which.path, which.body, from(machine))
		if res.Code != http.StatusTooManyRequests {
			continue
		}
		left, ok := statedField(res.Header(), httpx.RateLimitHeader,
			contracts.LoginAttemptsPolicyName, "t")
		if !ok {
			t.Fatalf("ask %d at the %s door = 429 stating no seconds left of the lockout's window: %q",
				ask, which.name, res.Header().Get(httpx.RateLimitHeader))
		}
		// The window opened at the first ask and runs window seconds from there, so
		// what is left of it is the window with no more than the time these asks took
		// taken off it, and never more than the window itself.
		spent := time.Since(opened)
		earliest, latest := window-spent-slack, window
		if d := time.Duration(left) * time.Second; d < earliest || d > latest {
			t.Errorf("ask %d at the %s door = 429 stating %d seconds left of the %s window that opened %s ago, want between %s and %s: the countdown of a shut door is the remainder of the window its own asks opened",
				ask, which.name, left, window, spent.Round(time.Millisecond),
				earliest.Round(time.Second), latest.Round(time.Second))
		}
		return
	}
	t.Fatalf("no ask was refused under the %d-attempt lockout of %s: two doors, %d asks, one counter",
		contracts.MaxAttempts, window, 2*contracts.MaxAttempts+1)
}
