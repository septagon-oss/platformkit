package internal_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// windowClosing stands for the one part of a RateLimit line two answers of one
// counter are entitled to differ in: the seconds left in the window, which tick
// between one request and the next. The quota left under the counter is not one of
// them, nor is the policy the line names.
var windowClosing = regexp.MustCompile(`;t=[0-9]+`)

// countedAnswer is what one sign-in answer states about the counter behind it, with
// that countdown set aside: the quota left, the ceilings named, and whether a
// Retry-After went on.
func countedAnswer(w *httptest.ResponseRecorder) string {
	retry := "none"
	if w.Header().Get(httpx.RetryAfterHeader) != "" {
		retry = "stated"
	}
	return windowClosing.ReplaceAllString(w.Header().Get(httpx.RateLimitHeader), ";t=") +
		" policy=" + w.Header().Get(httpx.RateLimitPolicyHeader) +
		" retry-after=" + retry
}

// The counter a failed sign-in leaves behind answers the same way about an address
// nobody knows, an address whose person a customer invited and who has chosen no
// password, and an address whose person did: attempt for attempt, the quota left,
// the policy named and whether a Retry-After went on are one reading for all three.
// The sentence beside the counter is compared elsewhere; what nothing compares is the
// number, and a counter that counted one kind of address differently, or named itself
// differently for one kind, would tell a stranger which of the three stood at the
// door while every answer beside it stayed as it was.
func TestTheCounterACountedSignInRefusalAnswersNamesNoKindOfAddress(t *testing.T) {
	const (
		unseen  = "nobody.behind.this.counter@acme.localhost"
		invited = "invited.who.chose.no.password.counts@acme.localhost"
		member  = "counted.out.after.choosing.a.password@acme.localhost"
		wrong   = "not the password anybody here was invited with"
	)
	router, conn, _ := mount(t, auth.OIDC{})
	somebodyWhoCannotSignInYet(t, conn, invited, false)
	person(t, conn, member)
	kinds := []struct{ addr, ip string }{
		{unseen, "203.0.113.211"}, {invited, "203.0.113.212"}, {member, "203.0.113.213"},
	}
	// One machine per address, so the only counter any of these three burns is its own.
	for attempt := 1; attempt <= contracts.MaxAttempts+1; attempt++ {
		answer := map[string]string{}
		for _, kind := range kinds {
			res := call(t, router, http.MethodPost, "/api/v1/auth/login",
				`{"email":"`+kind.addr+`","password":"`+wrong+`"}`, from(kind.ip))
			want := http.StatusUnauthorized
			if attempt > contracts.MaxAttempts {
				want = http.StatusTooManyRequests
			}
			if res.Code != want {
				t.Fatalf("%s attempt %d at the sign-in page = %d, want %d", kind.addr, attempt, res.Code, want)
			}
			answer[kind.addr] = countedAnswer(res)
		}
		if answer[unseen] != answer[invited] || answer[invited] != answer[member] {
			t.Errorf("attempt %d: the sign-in counter answers these three kinds of address differently, which is an enumeration oracle:\n  unseen:  %s\n  invited: %s\n  member:  %s",
				attempt, answer[unseen], answer[invited], answer[member])
		}
	}
}
