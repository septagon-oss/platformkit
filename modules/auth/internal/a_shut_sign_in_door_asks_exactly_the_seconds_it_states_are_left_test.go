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

// A shut sign-in door asks the caller to sit out the seconds its own answer states are
// left of the lockout's window: the countdown and the wait are two renderings of one
// reading (kit/httpx.sayCountedOnto writes `t` as the seconds rounded up and Retry-After
// as the same seconds with a floor of one), so an answer can disagree with itself only by
// rendering the two from different readings. No case beside this one says that equality
// out loud at a refusal which is not the first: the door cases compare the two numbers
// once, and the one case that brings a clock compares each number against itself across a
// wait — a question two renderings of one value always answer the same way, so it cannot
// see a wait that stops tracking its own countdown while both still fall. So this shuts
// the door, then asks twice more across real waits, requiring each shut answer to ask
// exactly the seconds it states are left and fewer than the shut answer before it.
func TestAShutSignInDoorAsksExactlyTheSecondsItStatesAreLeft(t *testing.T) {
	const (
		address = "one.wait.per.countdown@acme.localhost"
		machine = "203.0.113.251"
	)
	router, _, _ := mount(t, auth.OIDC{})
	wrong := func() *httptest.ResponseRecorder {
		return call(t, router, http.MethodPost, "/api/v1/auth/login",
			`{"email":"`+address+`","password":"no password anyone here was given"}`, from(machine))
	}
	ask := func(res *httptest.ResponseRecorder) (stated, waited int) {
		left, ok := statedField(res.Header(), httpx.RateLimitHeader,
			contracts.LoginAttemptsPolicyName, "t")
		if !ok {
			t.Fatalf("a shut door answered stating no seconds left of its window: %q",
				res.Header().Get(httpx.RateLimitHeader))
		}
		wait, err := strconv.Atoi(res.Header().Get(httpx.RetryAfterHeader))
		if err != nil {
			t.Fatalf("a shut door answered Retry-After %q, want the seconds it asks the caller to wait",
				res.Header().Get(httpx.RetryAfterHeader))
		}
		return left, wait
	}
	lastStated, lastWaited, shut := 0, 0, false
	for n := 1; n <= contracts.MaxAttempts+1 && !shut; n++ {
		res := wrong()
		if res.Code != http.StatusTooManyRequests {
			continue
		}
		lastStated, lastWaited = ask(res)
		if d := time.Duration(lastStated) * time.Second; d <= 0 || d > contracts.AttemptWindow {
			t.Fatalf("the ask that shut the door states %d seconds left, outside its own %s window",
				lastStated, contracts.AttemptWindow)
		}
		if lastWaited != lastStated {
			t.Errorf("the ask that shut the door states %d seconds left and asks %d: a shut door asks the wait its own countdown states",
				lastStated, lastWaited)
		}
		shut = true
	}
	if !shut {
		t.Fatalf("the password door never shut under the %d-attempt lockout of %s",
			contracts.MaxAttempts, contracts.AttemptWindow)
	}
	for round := 1; round <= 2; round++ {
		time.Sleep(time.Duration(round) * time.Second)
		stated, waited := ask(wrong())
		if waited != stated {
			t.Errorf("the %dth wait a shut door asked states %d seconds left and asks %d: the wait must track the countdown it is refused under",
				round, stated, waited)
		}
		if stated >= lastStated || waited >= lastWaited {
			t.Errorf("the shut door stated %d and asked %d, then after %ds still stated %d and asked %d: a refused attempt must not hold the window it refused under open",
				lastStated, lastWaited, round, stated, waited)
		}
		lastStated, lastWaited = stated, waited
	}
}
