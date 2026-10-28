package internal_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// The sign-in lockout's window opens at the failed attempt that first filled the counter
// and closes on its own instant, so the wait a shut door asks only ever falls: somebody
// who waits what the door told them is told a shorter wait by the next wrong password.
// A counter that re-anchored its window at each refused attempt instead answers with the
// whole window every time, and then everything on the answer agrees with everything else
// on that answer — the policy, its ceiling, the window beside the ceiling, the attempts
// left, the countdown and the wait that equals it — while the caller is told a wait no
// answer ever shortens. Only a clock outside the answers tells a closing window from a
// reopened one. So this shuts the password door, reads its countdown and the wait beside
// it, waits past a span of them, asks again, and compares the two shut answers: the
// second states a countdown and a wait shorter than the first by at least what the caller
// waited, and never the same numbers back.
func TestARefusedSignInDoesNotExtendTheLockoutsOwnWindow(t *testing.T) {
	const (
		address = "the.window.closes.at.its.own.instant@acme.localhost"
		machine = "203.0.113.252"
		wait    = 3 * time.Second
	)
	router, _, _ := mount(t, auth.OIDC{})
	wrong := func() *httptest.ResponseRecorder {
		return call(t, router, http.MethodPost, "/api/v1/auth/login",
			`{"email":"`+address+`","password":"no password anyone here was given"}`, from(machine))
	}
	// numbers are the two a shut door asks with: the seconds its counter says are left of
	// its window, and the seconds it asks the caller to sit out.
	numbers := func(res *httptest.ResponseRecorder, asked string) (int, int) {
		left, ok := statedField(res.Header(), httpx.RateLimitHeader,
			contracts.LoginAttemptsPolicyName, "t")
		if !ok {
			t.Fatalf("%s = 429 stating no seconds left of the lockout's window: %q",
				asked, res.Header().Get(httpx.RateLimitHeader))
		}
		retry := res.Header().Get(httpx.RetryAfterHeader)
		waited, err := strconv.Atoi(retry)
		if err != nil {
			t.Fatalf("%s = 429 stating Retry-After %q, want the seconds it asks the caller to wait", asked, retry)
		}
		return left, waited
	}
	var openingLeft, openingWait int
	shut := false
	for ask := 1; ask <= contracts.MaxAttempts+1; ask++ {
		res := wrong()
		if res.Code == http.StatusTooManyRequests {
			openingLeft, openingWait = numbers(res, "the ask that shut the door")
			shut = true
			break
		}
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("ask %d at the password door = %d, want it still letting the person try: %s",
				ask, res.Code, res.Body)
		}
	}
	if !shut {
		t.Fatalf("the password door never shut under the %d-attempt lockout of %s",
			contracts.MaxAttempts, contracts.AttemptWindow)
	}
	time.Sleep(wait)
	left, waited := numbers(wrong(), "the ask after the caller waited")
	if d := time.Duration(left) * time.Second; d < 0 || d > contracts.AttemptWindow {
		t.Errorf("the ask after the caller waited %s states %d seconds left, outside the lockout's own %s window",
			wait, left, contracts.AttemptWindow)
	}
	if left >= openingLeft || waited >= openingWait {
		t.Errorf("the door shut stating %d seconds left and asking %d, then the next refusal after %s still states %d and asks %d: a refused attempt must not extend the window it is refused under",
			openingLeft, openingWait, wait, left, waited)
	}
}
