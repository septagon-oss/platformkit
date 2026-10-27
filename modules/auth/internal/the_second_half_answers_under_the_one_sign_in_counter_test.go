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

// statedField reads one key out of the item of a Structured-Fields list that names
// this policy. The second result says the policy was named at all: an answer that
// consulted no counter states no item, and a reading of the empty list as zero would
// answer the question with the absence.
func statedField(h http.Header, field, policy, key string) (int, bool) {
	for _, item := range strings.Split(h.Get(field), ",") {
		name, params, ok := strings.Cut(item, ";")
		if !ok || strings.Trim(name, `"`) != policy {
			continue
		}
		for _, pair := range strings.Split(params, ";") {
			if v := strings.TrimPrefix(pair, key+"="); v != pair {
				n, err := strconv.Atoi(v)
				return n, err == nil
			}
		}
	}
	return 0, false
}

// The password door and the second-half door spend one sign-in counter, and the module
// says so: a script that switches from the password leg to the code leg is the same
// account under the same attack, reaches the same ceiling and buys nothing by changing
// doors. Both doors are expected to say that out loud — the same policy named under the
// same ceiling, one shared reading of what is left, and no wait until the counter both
// of them moved is out.
//
// Nothing compares the two doors: every sign-in case asks only the password leg, and
// every second-half case asks only the code leg. So a build that gave the code leg a
// counter of its own, or took the numbers off its answers while leaving the lockout,
// every status, body and cookie exactly as they were, answers green. This asks both
// doors from one machine about one address, one pair at a time, and reads one countdown
// out of both: the quota left falls by one ask after ask whichever door asked, the same
// ceiling is stated at both, and the door shuts on 429 at whichever door comes next once
// the counter is out — MaxAttempts asks between the two legs, not MaxAttempts each.
func TestTheSecondHalfAnswersUnderTheOneSignInCounterThePasswordDoorSpends(t *testing.T) {
	const (
		address = "one.counter.two.doors@acme.localhost"
		machine = "203.0.113.251"
		wrong   = "no password anyone here was given"
		code    = "000000"
	)
	router, _, _ := mount(t, auth.OIDC{})
	doors := []struct{ name, path, body string }{
		{"password", "/api/v1/auth/login", `{"email":"` + address + `","password":"` + wrong + `"}`},
		{"second half", "/api/v1/auth/challenge/verify", `{"email":"` + address + `","code":"` + code + `"}`},
	}
	left := contracts.MaxAttempts + 1
	for ask := 1; ask <= 2*contracts.MaxAttempts+1; ask++ {
		door := doors[(ask-1)%len(doors)]
		res := call(t, router, http.MethodPost, door.path, door.body, from(machine))
		shut := ask > contracts.MaxAttempts
		want := http.StatusUnauthorized
		if shut {
			want = http.StatusTooManyRequests
		}
		if res.Code != want {
			t.Fatalf("ask %d at the %s door = %d, want %d: the two doors spend one counter of %d together",
				ask, door.name, res.Code, want, contracts.MaxAttempts)
		}
		remaining, counted := statedField(res.Header(), httpx.RateLimitHeader,
			contracts.LoginAttemptsPolicyName, "r")
		if !counted {
			t.Fatalf("ask %d at the %s door = %d answering under no sign-in counter: RateLimit %q",
				ask, door.name, res.Code, res.Header().Get(httpx.RateLimitHeader))
		}
		if ceiling, capped := statedField(res.Header(), httpx.RateLimitPolicyHeader,
			contracts.LoginAttemptsPolicyName, "q"); !capped || ceiling != contracts.MaxAttempts {
			t.Errorf("ask %d at the %s door: `RateLimit-Policy` states no ceiling of %d for the sign-in counter: %q",
				ask, door.name, contracts.MaxAttempts, res.Header().Get(httpx.RateLimitPolicyHeader))
		}
		// One ask spends one attempt of the counter both doors share, so the reading
		// falls by one whichever door took it; a counter already at nought stays there
		// while the other door keeps being knocked on.
		wantLeft := left - 1
		if left == 0 {
			wantLeft = 0
		}
		if remaining != wantLeft {
			t.Errorf("ask %d at the %s door states %d attempts left; ask %d stated %d and one ask spends one attempt",
				ask, door.name, remaining, ask-1, left)
		}
		if waited := res.Header().Get(httpx.RetryAfterHeader) != ""; waited != shut {
			t.Errorf("ask %d at the %s door (%d): Retry-After %q, one wanted %v",
				ask, door.name, res.Code, res.Header().Get(httpx.RetryAfterHeader), shut)
		}
		left = remaining
	}
}
