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

// The sign-in counter has two doors, and the wait belongs to whichever one a refusal
// came out of. The cases beside this one cover each half separately: the two-door case
// compares a shut answer's wait with the seconds that same answer states are left, at
// both doors, but never twice across real time; the clock cases watch one countdown
// fall across real waits, but knock only on the password leg. So a build that kept the
// code leg's numbers internally consistent while pinning its wait to the whole window
// — a wait that states a smaller countdown each time and never asks a shorter one —
// answers every case beside this one green. This shuts the code leg, requires its shut
// answer to ask exactly the seconds it states are left, then asks again across real
// waits and requires the same answer of each later one and a wait shorter than the
// shut answer before it.
func TestAShutSecondHalfDoorAsksAWaitThatFallsWithItsOwnCountdown(t *testing.T) {
	const (
		address = "the.code.leg.waits.with.its.own.countdown@acme.localhost"
		machine = "203.0.113.253"
		code    = "000000"
	)
	router, _, _ := mount(t, auth.OIDC{})
	knock := func() *httptest.ResponseRecorder {
		return call(t, router, http.MethodPost, "/api/v1/auth/challenge/verify",
			`{"email":"`+address+`","code":"`+code+`"}`, from(machine))
	}
	// numbers are the two a shut door asks with: the seconds its counter says are left
	// of the lockout's window, and the seconds it asks the caller to sit out.
	numbers := func(res *httptest.ResponseRecorder, asked string) (stated, waited int) {
		left, ok := statedField(res.Header(), httpx.RateLimitHeader,
			contracts.LoginAttemptsPolicyName, "t")
		if !ok {
			t.Fatalf("%s = 429 stating no seconds left of the lockout's window: RateLimit %q",
				asked, res.Header().Get(httpx.RateLimitHeader))
		}
		wait, err := strconv.Atoi(res.Header().Get(httpx.RetryAfterHeader))
		if err != nil {
			t.Fatalf("%s = 429 stating Retry-After %q, want the seconds it asks the caller to wait",
				asked, res.Header().Get(httpx.RetryAfterHeader))
		}
		return left, wait
	}
	lastStated, lastWaited, shut := 0, 0, false
	for ask := 1; ask <= contracts.MaxAttempts+1 && !shut; ask++ {
		res := knock()
		if res.Code == http.StatusTooManyRequests {
			lastStated, lastWaited = numbers(res, "the ask that shut the code leg")
			shut = true
		}
	}
	if !shut {
		t.Fatalf("the code leg never shut under the %d-attempt lockout of %s",
			contracts.MaxAttempts, contracts.AttemptWindow)
	}
	for round := 1; round <= 2; round++ {
		time.Sleep(time.Duration(round) * time.Second)
		stated, waited := numbers(knock(), "the wait a shut code leg asked")
		if waited != stated {
			t.Errorf("the %dth wait a shut code leg asked states %d seconds left and asks %d: a shut door asks the wait its own countdown states",
				round, stated, waited)
		}
		if stated >= lastStated || waited >= lastWaited {
			t.Errorf("the shut code leg stated %d and asked %d, then after %ds still stated %d and asked %d: a refused attempt must not hold the window it refused under open",
				lastStated, lastWaited, round, stated, waited)
		}
		lastStated, lastWaited = stated, waited
	}
}
