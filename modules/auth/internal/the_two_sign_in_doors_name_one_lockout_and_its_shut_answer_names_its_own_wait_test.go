package internal_test

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// The two password doors of this module — the sign-in and the second-half leg — say they
// stand under one lockout: the same policy, the same ceiling, the same window, and the
// wait of a shut door belongs to that counter. A reading that looks this policy's name up
// among several answers cannot see a second counter arrive beside it, and one that reads
// only the attempts left cannot see a door hand back a wait of its own. So this asks both
// doors from one machine about one address until the lockout is out, and on every answer
// compares what it names, not what it means: `RateLimit-Policy` is this one policy's item
// and nothing else, and on each answer that is shut the seconds the counter says are left
// of its own window are the seconds `Retry-After` says to wait.
func TestTheTwoSignInDoorsNameOneLockoutAndItsShutAnswerNamesItsOwnWait(t *testing.T) {
	const (
		address = "one.lockout.two.doors@acme.localhost"
		machine = "203.0.113.253"
	)
	oneLockout := `"` + contracts.LoginAttemptsPolicyName + `";q=` +
		strconv.Itoa(contracts.MaxAttempts) + ";w=" +
		strconv.Itoa(int(contracts.AttemptWindow/time.Second))
	router, _, _ := mount(t, auth.OIDC{})
	doors := []struct{ name, path, body string }{
		{"password", "/api/v1/auth/login", `{"email":"` + address + `","password":"no password anyone here was given"}`},
		{"second half", "/api/v1/auth/challenge/verify", `{"email":"` + address + `","code":"000000"}`},
	}
	for ask := 1; ask <= 2*contracts.MaxAttempts+1; ask++ {
		door := doors[(ask-1)%len(doors)]
		res := call(t, router, http.MethodPost, door.path, door.body, from(machine))
		if got := res.Header().Get(httpx.RateLimitPolicyHeader); got != oneLockout {
			t.Fatalf("ask %d at the %s door: `RateLimit-Policy` = %q, want this one lockout's item alone, %q: a second counted knob at this door is a second knob for one policy",
				ask, door.name, got, oneLockout)
		}
		if res.Code != http.StatusTooManyRequests {
			continue
		}
		// A shut answer states the window it is refused under in the same breath as the
		// wait it asks for; the two come apart the moment a door is refused by a counter
		// other than the one it names.
		var remaining, left int
		if _, err := fmt.Sscanf(res.Header().Get(httpx.RateLimitHeader),
			`"`+contracts.LoginAttemptsPolicyName+`";r=%d;t=%d`, &remaining, &left); err != nil {
			t.Fatalf("ask %d at the %s door = 429 stating %q, want the lockout's quota left and its window's seconds left: %v",
				ask, door.name, res.Header().Get(httpx.RateLimitHeader), err)
		}
		wait := res.Header().Get(httpx.RetryAfterHeader)
		if want := strconv.Itoa(left); wait != want || left < 1 || left > int(contracts.AttemptWindow/time.Second) {
			t.Errorf("ask %d at the %s door = 429 asking Retry-After=%q while the same answer states %d seconds left of the %d-second lockout: the wait of a shut door is that counter's own window",
				ask, door.name, wait, left, int(contracts.AttemptWindow/time.Second))
		}
	}
}
