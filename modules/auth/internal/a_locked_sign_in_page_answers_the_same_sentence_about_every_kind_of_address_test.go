package internal_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// A shut sign-in page answers one sentence about every kind of address it locks out, and
// that sentence is the whole of what a stranger learns from the lockout: an address nobody
// knows, an address whose person a customer invited and who has chosen no password, and an
// address whose person did choose one all answer the same body, and none of the three is
// signed in by the refusal.
//
// The refusals before the lockout are compared byte for byte elsewhere, and the lockout's
// own error is read at the service elsewhere, but nothing reads the body the 429 carries. A
// build whose lockout said which kind of address it had shut — "that invitation is still
// waiting for its link", say — would leave every case beside this one green while the door
// answered ten guesses with the list of addresses this customer has already invited.
func TestALockedSignInPageAnswersTheSameSentenceAboutEveryKindOfAddress(t *testing.T) {
	const (
		unseen  = "nobody.behind.this.lockout@acme.localhost"
		invited = "invited.who.chose.no.password@acme.localhost"
		member  = "locked.out.after.choosing.a.password@acme.localhost"
		wrong   = "not the password anybody here was invited with"
	)
	router, conn, _ := mount(t, auth.OIDC{})
	somebodyWhoCannotSignInYet(t, conn, invited, false)
	person(t, conn, member)
	// One machine per address, so the only counter any of these three burns is its own.
	locked := map[string]string{}
	for _, kind := range []struct{ addr, ip string }{
		{unseen, "203.0.113.201"}, {invited, "203.0.113.202"}, {member, "203.0.113.203"},
	} {
		ask := func(password string) *httptest.ResponseRecorder {
			return call(t, router, http.MethodPost, "/api/v1/auth/login",
				`{"email":"`+kind.addr+`","password":"`+password+`"}`, from(kind.ip))
		}
		for failure := range contracts.MaxAttempts {
			if got := ask(wrong); got.Code != http.StatusUnauthorized {
				t.Fatalf("%s failure %d at the sign-in page = %d, want the 401 of a wrong answer that was counted",
					kind.addr, failure+1, got.Code)
			}
		}
		res := ask(wrong)
		if res.Code != http.StatusTooManyRequests {
			t.Fatalf("%s answered %d after the %d failures it was given, want the lockout every kind of address earns there",
				kind.addr, res.Code, contracts.MaxAttempts)
		}
		if cookie := sessionCookie(res); cookie != "" {
			t.Errorf("the lockout signed %s in: it set a session cookie %s", kind.addr, cookie)
		}
		locked[kind.addr] = withoutInstance(res.Body.String())
	}
	if locked[unseen] != locked[invited] || locked[invited] != locked[member] {
		t.Errorf("the three lockouts differ, which is an enumeration oracle:\n  unseen:  %s\n  invited: %s\n  member:  %s",
			locked[unseen], locked[invited], locked[member])
	}
}
