package admin_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// TestTheSessionsButtonSaysWhatTheClickEnds pins the one sentence on the screen
// that is a number rather than a name. "End the other N" is the module's count
// of the rows that are not the one answering, and sessionsPage takes it from
// `items[0].Current` alone. The order the command returns is last_seen_at, and
// the module slides that column at most once per SessionTouch — five minutes —
// so the reading session legitimately sorts behind one that was used more
// recently: the current row can be second, the head test misses it, and the
// button claims one more ending than the command performs.
//
// The assertion is the honest one, independent of how a fix counts: press the
// button the person read and the number of sessions that ended is the number
// the button said.
func TestTheSessionsButtonSaysWhatTheClickEnds(t *testing.T) {
	store := threeSessions()
	// The order the module actually returns: the phone used a minute ago, then
	// the laptop this person is reading on — whose own last_seen is older than
	// the phone's because the five-minute throttle skipped its slide.
	store.items = []*authcontracts.SessionListing{
		store.items[1], // ref-there, not current, most recently seen
		store.items[0], // ref-here, the reading session, current, second in the list
	}
	router := mountAs(t, caller{}, withSessions(store))

	res := signedInAs(t, router, http.MethodGet, "/app/auth/sessions", "")
	if res.Code != http.StatusOK {
		t.Fatalf("the sessions screen = %d %s", res.Code, res.Body.String())
	}
	body := res.Body.String()
	at := strings.Index(body, "End the other ")
	if at < 0 {
		t.Fatal("the screen offers no \"End the other N\" button for a list with another session in it")
	}
	digits := body[at+len("End the other "):]
	end := 0
	for end < len(digits) && digits[end] >= '0' && digits[end] <= '9' {
		end++
	}
	claimed, err := strconv.Atoi(digits[:end])
	if err != nil {
		t.Fatalf("the button's count is not a number: %q", digits[:8])
	}

	store.revoked = nil
	res = signedInAs(t, router, http.MethodPost, "/app/auth/sessions/revoke-rest", "")
	if res.Code != http.StatusSeeOther {
		t.Fatalf("ending the other sessions = %d %s, want 303", res.Code, res.Body.String())
	}
	if len(store.revoked) != claimed {
		t.Errorf("the button said %d and the click ended %d: the person agreed to a different number than the command performed",
			claimed, len(store.revoked))
	}
}
