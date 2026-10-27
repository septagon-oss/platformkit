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

// statedReading reads one policy's number out of a Structured-Fields list of
// `"name";key=value;…` items: the attempts left in `RateLimit`, the ceiling the same
// policy states in `RateLimit-Policy`. The second result says the policy was named in
// that list at all — an answer under no counter states no number, and a reading of it
// as zero would answer the question with the absence.
func statedReading(h http.Header, field, policy, key string) (int, bool) {
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

// A wrong password the door still lets the person try again answers under its counter,
// and says so: the sign-in policy named in `RateLimit` with the attempts left, the same
// policy's ceiling in `RateLimit-Policy`, and no `Retry-After` — the wait belongs to the
// answer that refused for want of the counter, not to this one. The attempts left fall
// from one try to the next and reach nought exactly when the door shuts on 429, because
// that number is how a caller knows how many tries remain; an answer naming no counter,
// or the same number every time, leaves it to retry on a guess.
func TestACountedSignInRefusalStatesTheAttemptsItsCounterHasLeft(t *testing.T) {
	const (
		address = "counted.down.to.no.attempts.left@acme.localhost"
		machine = "203.0.113.241"
		wrong   = "no password anyone here was given"
	)
	router, _, _ := mount(t, auth.OIDC{})
	left := contracts.MaxAttempts + 1
	for attempt := 1; attempt <= contracts.MaxAttempts+1; attempt++ {
		res := call(t, router, http.MethodPost, "/api/v1/auth/login",
			`{"email":"`+address+`","password":"`+wrong+`"}`, from(machine))
		shut := attempt > contracts.MaxAttempts
		want := http.StatusUnauthorized
		if shut {
			want = http.StatusTooManyRequests
		}
		if res.Code != want {
			t.Fatalf("attempt %d at the sign-in page = %d, want %d", attempt, res.Code, want)
		}
		remaining, counted := statedReading(res.Header(), httpx.RateLimitHeader, contracts.LoginAttemptsPolicyName, "r")
		if !counted {
			t.Fatalf("attempt %d = %d answering under no sign-in counter: RateLimit %q",
				attempt, res.Code, res.Header().Get(httpx.RateLimitHeader))
		}
		ceiling, capped := statedReading(res.Header(), httpx.RateLimitPolicyHeader, contracts.LoginAttemptsPolicyName, "q")
		if !capped || ceiling != contracts.MaxAttempts {
			t.Errorf("attempt %d: `RateLimit-Policy` states no ceiling of %d for the sign-in counter: %q",
				attempt, contracts.MaxAttempts, res.Header().Get(httpx.RateLimitPolicyHeader))
		}
		if remaining >= left {
			t.Errorf("attempt %d: the sign-in counter answers %d attempts left, no fewer than attempt %d's %d",
				attempt, remaining, attempt-1, left)
		}
		waited := res.Header().Get(httpx.RetryAfterHeader) != ""
		if waited != shut {
			t.Errorf("attempt %d (%d): Retry-After %q, one wanted %v", attempt, res.Code, res.Header().Get(httpx.RetryAfterHeader), shut)
		}
		if shut && remaining != 0 {
			t.Errorf("attempt %d: the door shut on 429 with the sign-in counter at %d attempts left", attempt, remaining)
		}
		left = remaining
	}
}
