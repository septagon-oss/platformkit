package rest_test

// A person who runs no browser script goes exactly where the answer to their press
// says, because nothing on the page asks again. The answer to recording a set leads
// to the pause that set owes or to the pair after it; the answer to ending the walk
// is the other half of that promise, and it has to lead to the summary the finish
// returned — not to an address the session holds no page for, which would strand
// the person who cannot ask again. This walks one fixture walk to its end by pressing
// each served page's own form, presses the finish form that end carries, and follows
// the address that press answers with the way a browser does. It asserts through
// addresses, status codes and the outbox count.

import (
	"fmt"
	"strings"
	"testing"
)

// walkGuidedPresses presses the completion form of every served page of one walk, in
// the order its sets come, following each press's own answer to a page. It returns
// the address and body of the page the walk stands on once nothing is left to record,
// which is where a person meets the finish.
func walkGuidedPresses(t *testing.T, h guidedHarness, web string) (string, string) {
	t.Helper()
	session := guidedFixture()
	for position := range 4 {
		resume := guidedCall(h.router, "GET", web, "", "", "")
		if resume.Code != 303 {
			t.Fatalf("position %d: the walk answered %d, not the step it stands on", position, resume.Code)
		}
		page := guidedCall(h.router, "GET", resume.Header().Get("Location"), "", "", "")
		if page.Code != 200 {
			t.Fatalf("position %d: the page at %s answered %d", position, resume.Header().Get("Location"), page.Code)
		}
		step, set, ok := session.Pair(position)
		if !ok {
			t.Fatalf("the fixture walk holds no pair at position %d", position)
		}
		action, fields := servedForm(t, page.Body.String(), "/complete-set")
		if got := fields.Get("setId"); got != session.Steps[step].Sets[set].String() {
			t.Fatalf("position %d: the page posts set %q, not %q, the set it stands on", position, got, session.Steps[step].Sets[set])
		}
		res := guidedCall(h.router, "POST", action, fields.Encode(), "application/x-www-form-urlencoded", "")
		if res.Code != 303 {
			t.Fatalf("position %d: the press of the set the page carries answered %d", position, res.Code)
		}
		_, at, body := guidedPressAnswer(t, h, res.Header().Get("Location"))
		if body == "" {
			t.Fatalf("position %d: the address the press answered with, %s, is no page", position, at)
		}
	}
	resume := guidedCall(h.router, "GET", web, "", "", "")
	if resume.Code != 303 {
		t.Fatalf("a walk with every set recorded answered %d, not its finish", resume.Code)
	}
	page := guidedCall(h.router, "GET", resume.Header().Get("Location"), "", "", "")
	if page.Code != 200 {
		t.Fatalf("the page at %s answered %d", resume.Header().Get("Location"), page.Code)
	}
	return resume.Header().Get("Location"), page.Body.String()
}

// guidedPressAnswer walks the chain of addresses one reply names until something
// answers with a page, and reports that page's status, its address and its body. An
// empty body says the chain ended on something that is not a page.
func guidedPressAnswer(t *testing.T, h guidedHarness, at string) (int, string, string) {
	t.Helper()
	trail := []string{}
	for range 6 {
		res := guidedCall(h.router, "GET", at, "", "", "")
		trail = append(trail, fmt.Sprintf("%d %s", res.Code, at))
		if res.Code == 303 {
			at = res.Header().Get("Location")
			continue
		}
		if res.Code != 200 {
			t.Errorf("the chain of addresses a press answered with ends at %d, not at a page: %v", res.Code, trail)
			return res.Code, at, ""
		}
		return res.Code, at, res.Body.String()
	}
	t.Fatalf("the chain of addresses never answered with a page: %v", trail)
	return 0, "", ""
}

// TestTheAnswerToAGuidedFinishPressLeadsToTheSummary presses the finish form the end
// of one walk carries and follows the address that press answers with: it has to be
// that session's summary, it has to answer as a page, and the one finish event has to
// commit with the walk it ends.
func TestTheAnswerToAGuidedFinishPressLeadsToTheSummary(t *testing.T) {
	h := newGuided(t)
	mountGuidedScreens(h)
	web := "/app/practice/sessions/" + guidedID.String()

	at, body := walkGuidedPresses(t, h, web)
	if !strings.HasSuffix(at, "/finish") {
		t.Fatalf("a walk with every set recorded stands at %s, not at its finish", at)
	}
	if got := h.events(t); got != 4 {
		t.Fatalf("the four presses wrote %d events, want the four sets they recorded", got)
	}

	action, fields := servedForm(t, body, "/finish")
	res := guidedCall(h.router, "POST", action, fields.Encode(), "application/x-www-form-urlencoded", "")
	if res.Code != 303 {
		t.Fatalf("the finish fields the page itself carries, posted unchanged, answered %d", res.Code)
	}
	answer := res.Header().Get("Location")
	if want := web + "/summary"; answer != want {
		t.Errorf("the press of the finish answered with %s, not the summary of the walk it ended (%s)", answer, want)
	}
	if _, _, page := guidedPressAnswer(t, h, answer); page == "" {
		t.Errorf("the address the finish press answered with, %s, is no page a person who runs no script could land on", answer)
	}
	if got := h.events(t); got != 5 {
		t.Errorf("the finished walk wrote %d events, want the four recorded sets and the one finish", got)
	}
}
