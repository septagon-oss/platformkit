package rest_test

// A person who runs no browser script moves through a guided walk one press at a
// time: each screen sends back the pair that screen carries, and the walk answers
// with the next one. Pressing the first page of a walk cannot tell that promise
// apart from a page that always names its step's first set, because on that page the
// two are the same set, and the press of a set already recorded answers as the
// accepted answer to the first one — no error, no second event, and no move. Only a
// page standing on a step's later set separates them. This presses the served pages
// of one walk from its first screen to its finish, and after every press asks the
// walk where it now stands, so a page that hides the wrong pair is seen at the tier
// where the command really records.

import (
	"net/url"
	"strings"
	"testing"
)

// servedStep asks the walk where it stands and returns that page's address and body:
// the resume a browser would follow, then the page it renders.
func servedStep(t *testing.T, h guidedHarness, web string) (string, string) {
	t.Helper()
	resume := guidedCall(h.router, "GET", web, "", "", "")
	if resume.Code != 303 {
		t.Fatalf("the walk answered %d instead of the step it stands on", resume.Code)
	}
	at := resume.Header().Get("Location")
	page := guidedCall(h.router, "GET", at, "", "", "")
	if page.Code != 200 {
		t.Fatalf("the page at %s answered %d", at, page.Code)
	}
	return at, page.Body.String()
}

// TestTheServedPagesOfAWalkPressedFormByFormReachItsFinish presses each served step
// page's own completion form, in the order a person meets the sets, and then the
// served finish page's own form. Each press has to be the set the page showed: the
// walk moves to the next pair after every one, and one event commits with it.
func TestTheServedPagesOfAWalkPressedFormByFormReachItsFinish(t *testing.T) {
	h := newGuided(t)
	mountGuidedScreens(h)
	web := "/app/practice/sessions/" + guidedID.String()
	walk := guidedFixture()

	pressed := 0
	for position := range walk.Steps {
		step := walk.Steps[position]
		for index := range step.Sets {
			at, body := servedStep(t, h, web)
			if !strings.HasSuffix(at, step.Sets[index].String()) {
				t.Fatalf("with %d sets recorded the walk stands at %s, not on set %s of step %q",
					pressed, at, step.Sets[index], step.Title)
			}
			action, fields := servedForm(t, body, "/complete-set")
			if got := fields.Get("stepId"); got != step.ID.String() {
				t.Errorf("the page of step %q posts step %q, not the step it shows", step.Title, got)
			}
			if got := fields.Get("setId"); got != step.Sets[index].String() {
				t.Errorf("the page of set %d of %d of step %q posts set %q, so its press repeats a set already recorded",
					index+1, len(step.Sets), step.Title, got)
			}
			res := guidedCall(h.router, "POST", action, fields.Encode(), "application/x-www-form-urlencoded", "")
			if res.Code != 303 {
				t.Fatalf("the completion fields the page itself carries, posted unchanged, answered %d: %s", res.Code, res.Body.String())
			}
			pressed++
			if h.events(t) != pressed {
				t.Fatalf("the press of set %s wrote %d events, want the one recorded set", step.Sets[index], h.events(t))
			}
		}
	}

	at, body := servedStep(t, h, web)
	if !strings.HasSuffix(at, "/finish") {
		t.Fatalf("a walk with every one of its %d sets pressed stands at %s, not at its finish", pressed, at)
	}
	action, fields := servedForm(t, body, "/finish")
	if _, err := url.ParseQuery(fields.Encode()); err != nil {
		t.Fatalf("the finish page posts fields no form could encode: %v", err)
	}
	res := guidedCall(h.router, "POST", action, fields.Encode(), "application/x-www-form-urlencoded", "")
	if res.Code != 303 {
		t.Errorf("the finish fields the page itself carries, posted unchanged, answered %d: %s", res.Code, res.Body.String())
	}
	if h.events(t) != pressed+1 {
		t.Errorf("the finished walk wrote %d events, want the %d recorded sets and the one finish", h.events(t), pressed)
	}
}
