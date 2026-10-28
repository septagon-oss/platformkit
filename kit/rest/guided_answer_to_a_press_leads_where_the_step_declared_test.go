package rest_test

// The address a completion answers with is the whole of where a person who runs no
// browser script goes next: the browser follows that address and never asks the
// session again. A step declares its pause after one particular set, so the answer
// to recording that set has to lead to that set's pause, and where the step declares
// none the person has to be left on the pair that comes next. Either way the answer
// has to lead to a page, not to a pair the session does not hold. This presses each
// set's own form and follows the address that press names.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
)

// guidedRestOwed is the pause a session declares after one of its pairs: the seconds
// its step names between two sets, or after the last set of that step.
func guidedRestOwed(session entity.GuidedSession, step, index int) int32 {
	if index+1 < len(session.Steps[step].Sets) {
		return session.Steps[step].RestBetweenSeconds
	}
	return session.Steps[step].RestAfterSeconds
}

// followGuidedAnswer walks the chain of addresses one reply names until something
// answers with a page, and keeps the chain so that a refusal says where it began.
func followGuidedAnswer(t *testing.T, h guidedHarness, at string) (int, string, []string) {
	t.Helper()
	trail := []string{}
	for range 5 {
		res := guidedCall(h.router, "GET", at, "", "", "")
		trail = append(trail, fmt.Sprintf("%d %s", res.Code, at))
		if res.Code != 303 {
			return res.Code, at, trail
		}
		at = res.Header().Get("Location")
	}
	t.Fatalf("the walk never answered with a page: %v", trail)
	return 0, "", trail
}

// TestTheAnswerToAGuidedPressLeadsToThePageThatSetOwes presses the completion form
// of every page of one fixture walk and follows the address that press answers with:
// where the step declares a pause after the set just recorded, that address is the
// pause of that set and answers as a page; where it declares none, the person is
// left standing on the pair after the one they recorded.
func TestTheAnswerToAGuidedPressLeadsToThePageThatSetOwes(t *testing.T) {
	h := newGuided(t)
	mountGuidedScreens(h)
	web := "/app/practice/sessions/" + guidedID.String()
	session := guidedFixture()

	for position := range 4 {
		step, set, ok := session.Pair(position)
		if !ok {
			t.Fatalf("the fixture walk holds no pair at position %d", position)
		}
		pair := session.Steps[step].Sets[set]

		resume := guidedCall(h.router, "GET", web, "", "", "")
		if resume.Code != 303 {
			t.Fatalf("position %d: the walk answered %d, not the step it stands on", position, resume.Code)
		}
		page := guidedCall(h.router, "GET", resume.Header().Get("Location"), "", "", "")
		if page.Code != 200 {
			t.Fatalf("position %d: the page at %s answered %d", position, resume.Header().Get("Location"), page.Code)
		}
		action, fields := servedForm(t, page.Body.String(), "/complete-set")
		if got := fields.Get("setId"); got != pair.String() {
			t.Fatalf("position %d: the page posts set %q, not %q, the set it stands on", position, got, pair)
		}

		res := guidedCall(h.router, "POST", action, fields.Encode(), "application/x-www-form-urlencoded", "")
		if res.Code != 303 {
			t.Fatalf("position %d: the press of the set the page carries answered %d: %s", position, res.Code, res.Body.String())
		}
		code, at, trail := followGuidedAnswer(t, h, res.Header().Get("Location"))
		if code != 200 {
			t.Errorf("position %d: the address the press answered with leads to %d, not to a page: %v", position, code, trail)
			continue
		}
		if owed := guidedRestOwed(session, step, set); owed > 0 {
			if want := web + "/rest/" + session.Steps[step].ID.String() + "/" + pair.String(); at != want {
				t.Errorf("position %d declares %d seconds of pause after it, so its press had to lead to %s and led to %s: %v", position, owed, want, at, trail)
			}
			continue
		}
		next, _, ok := session.Pair(position + 1)
		if !ok {
			t.Fatalf("the fixture walk holds no pair after position %d", position)
		}
		if want := "/steps/" + session.Steps[next].ID.String() + "/sets/"; !strings.Contains(at, want) {
			t.Errorf("position %d declares no pause, so its press had to leave the person on the pair after it and left them at %s: %v", position, at, trail)
		}
	}

	if got := h.events(t); got != 4 {
		t.Errorf("the four presses wrote %d events, want the four sets they recorded", got)
	}
}
