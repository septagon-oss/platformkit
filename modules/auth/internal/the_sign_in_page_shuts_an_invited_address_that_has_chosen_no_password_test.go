package internal_test

import (
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// The sign-in page shuts an address whose person a customer invited and who has chosen
// no password, on the failures counted against that address at the page, and answers the
// allowance it spent with the same refusal every other kind of address earns there.
//
// The other two kinds are measured at this route — an address nobody knows by
// TestPasswordLockoutStatesTheLimitThatRefusedIt, a member by
// TestARefusalIsRecordedOncePerWindow — and the invited one is measured at the service
// alone, so nothing at the door asks that it costs its failures too. A build that
// refused an address which cannot sign in without counting it, the plausible "do not
// spend a stranger's guesses on accounts with no password yet", keeps every case beside
// this one green and answers this one with 401.
func TestTheSignInPageShutsAnInvitedAddressThatHasChosenNoPassword(t *testing.T) {
	const waiting = "no.password.chosen@acme.localhost"
	router, conn, _ := mount(t, auth.OIDC{})
	somebodyWhoCannotSignInYet(t, conn, waiting, false)
	ask := func(password string) int {
		return call(t, router, http.MethodPost, "/api/v1/auth/login",
			`{"email":"`+waiting+`","password":"`+password+`"}`, from("203.0.113.24")).Code
	}
	for failure := range contracts.MaxAttempts {
		if got := ask("not the password anybody was invited with"); got != http.StatusUnauthorized {
			t.Fatalf("failure %d at the sign-in page for %s = %d, want the 401 one wrong answer gets: the lockout "+
				"below is only worth reading if these failures were counted", failure+1, waiting, got)
		}
	}
	if got := ask("a sufficiently strong password"); got != http.StatusTooManyRequests {
		t.Errorf("the sign-in page answered %s with %d after the %d failures it was given, want 429: an address "+
			"whose person is invited costs its failures as every other address does, or the shut door says which "+
			"addresses this customer has already invited", waiting, got, contracts.MaxAttempts)
	}
}
