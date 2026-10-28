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

// One counter stands behind both doors of a sign-in, so at one instant the two doors
// state one remainder of one window and ask one wait. Every comparison this module
// makes between the doors sits inside a single answer, and every comparison across
// real time sits inside one door, so neither kind can see a door that holds a
// remainder of its own: one that shortens the wait it asks by a fixed span while still
// counting down answers its own two numbers equal, still falls, and tells the same
// person two waits a span apart depending on which door they knocked. So this shuts
// the counter through both doors, then asks both doors at two instants a real span
// apart: the doors agree on both numbers to within the seconds the two asks themselves
// can round or stall apart, and no door's numbers rise between the instants.
func TestTheTwoShutDoorsAskOneWaitAsTheirSharedCountdownFalls(t *testing.T) {
	const (
		address = "one.countdown.answered.at.both.doors@acme.localhost"
		machine = "203.0.113.249"
		// apart is how far the two doors may sit at one instant. The two asks run
		// milliseconds apart, and one remainder rounded up either side of a whole
		// second differs by a second, with a second more standing a garbage collection
		// or a busy database between them: a wider gap is a second countdown.
		apart = 2
	)
	type answer struct{ stated, waited int }
	router, _, _ := mount(t, auth.OIDC{})
	doors := []struct{ name, path, body string }{
		{"password", "/api/v1/auth/login", `{"email":"` + address + `","password":"no password anyone here was given"}`},
		{"code", "/api/v1/auth/challenge/verify", `{"email":"` + address + `","code":"000000"}`},
	}
	knock := func(door int) *httptest.ResponseRecorder {
		return call(t, router, http.MethodPost, doors[door].path, doors[door].body, from(machine))
	}
	// numbers reads the two figures a shut door answers with: the seconds its counter
	// says are left of the lockout's window, and the seconds it asks the caller to sit out.
	numbers := func(res *httptest.ResponseRecorder, door string) answer {
		left, ok := statedField(res.Header(), httpx.RateLimitHeader,
			contracts.LoginAttemptsPolicyName, "t")
		if !ok {
			t.Fatalf("the shut %s door stated no seconds left of the lockout's window: RateLimit %q",
				door, res.Header().Get(httpx.RateLimitHeader))
		}
		wait, err := strconv.Atoi(res.Header().Get(httpx.RetryAfterHeader))
		if err != nil {
			t.Fatalf("the shut %s door stated Retry-After %q, want the seconds it asks the caller to wait",
				door, res.Header().Get(httpx.RetryAfterHeader))
		}
		return answer{stated: left, waited: wait}
	}
	// The window opens on the first refused ask, so knocking each door in turn shuts
	// both of them under one counter. A lockout the two doors did not share would leave
	// one door answering the wrong status here, which another case holds as its fact.
	shut := []bool{false, false}
	for ask := 1; ask <= 2*contracts.MaxAttempts+1; ask++ {
		door := (ask - 1) % len(doors)
		if !shut[door] && knock(door).Code == http.StatusTooManyRequests {
			shut[door] = true
		}
		if shut[0] && shut[1] {
			break
		}
	}
	if !shut[0] || !shut[1] {
		t.Fatalf("the lockout of %d attempts left %s shut and %s open under %d asks of both doors",
			contracts.MaxAttempts, doors[0].name, doors[1].name, 2*contracts.MaxAttempts+1)
	}
	var last [2]answer
	for instant := 1; instant <= 2; instant++ {
		time.Sleep(time.Duration(instant) * time.Second)
		got := [2]answer{}
		for door, d := range doors {
			res := knock(door)
			if res.Code != http.StatusTooManyRequests {
				t.Fatalf("the %s door answered %d at the %dth instant, having shut under the %d-attempt lockout of %s",
					d.name, res.Code, instant, contracts.MaxAttempts, contracts.AttemptWindow)
			}
			got[door] = numbers(res, d.name)
		}
		if max(got[0].stated, got[1].stated)-min(got[0].stated, got[1].stated) > apart {
			t.Errorf("one instant, two doors: the %s door states %d seconds left of the shared window and the %s door states %d: one counter has one remainder to state",
				doors[0].name, got[0].stated, doors[1].name, got[1].stated)
		}
		if max(got[0].waited, got[1].waited)-min(got[0].waited, got[1].waited) > apart {
			t.Errorf("one instant, two doors: the %s door asks %d seconds and the %s door asks %d: a shut caller sits out one wait, whichever door refused them",
				doors[0].name, got[0].waited, doors[1].name, got[1].waited)
		}
		if instant == 2 {
			for door, d := range doors {
				if got[door].stated > last[door].stated || got[door].waited > last[door].waited {
					t.Errorf("the %s door stated %d and asked %d, then %ds later stated %d and asked %d: the wait of a shut door does not grow while its counter runs down",
						d.name, last[door].stated, last[door].waited, instant,
						got[door].stated, got[door].waited)
				}
			}
		}
		last = got
	}
}
