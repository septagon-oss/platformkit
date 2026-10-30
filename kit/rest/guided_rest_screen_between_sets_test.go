package rest_test

// The rest screen is the pause a session declares after a set: a countdown that
// carries on from the moment that set was recorded, "+15 s" on top of it, and the
// step that comes next. The pause belongs to one pair of the session, so a person
// who has not recorded that set is met with the session, not with a countdown
// that was never owed to them.

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// recordGuidedSets completes the first sets of the fixture session through the
// same native form a no-JavaScript person sends.
func recordGuidedSets(t *testing.T, h guidedHarness, web string, positions ...int) {
	t.Helper()
	for _, position := range positions {
		in := guidedInput(position, int64(position+1))
		form := url.Values{"stepId": {in.StepID.String()}, "setId": {in.SetID.String()}, "ifRevision": {fmt.Sprint(in.IfRevision)}}.Encode()
		res := guidedCall(h.router, "POST", web+"/complete-set", form, "application/x-www-form-urlencoded", "")
		if res.Code != 303 {
			t.Fatalf("set %d: %d %s", position, res.Code, res.Body.String())
		}
	}
}

// TestTheRestScreenCountsDownFromTheSetItFollows checks that the countdown is
// anchored to the set it follows rather than to the request that opened the page:
// the deadline a browser counts against is that set's own recorded moment plus
// the seconds the step declares, so the screen says the same second it said
// before however often it is reopened. The "+15 s" is added to that same screen
// and asks for the next fifteen on top of what it already added, and the pair
// after the one the pause belongs to is named as what is up next.
func TestTheRestScreenCountsDownFromTheSetItFollows(t *testing.T) {
	h := newGuided(t)
	mountGuidedScreens(h)
	web := "/app/practice/sessions/" + guidedID.String()
	recordGuidedSets(t, h, web, 0, 1, 2)

	kept := h.read(t)
	if len(kept.Progress.Completed) != 3 {
		t.Fatalf("three sets were recorded, the session keeps %d", len(kept.Progress.Completed))
	}
	step, set := kept.Steps[1], kept.Steps[1].Sets[1]
	at := fmt.Sprintf("%s/rest/%s/%s", web, step.ID, set)
	page := guidedCall(h.router, "GET", at, "", "", "")
	if page.Code != 200 {
		t.Fatalf("the pause after a recorded set: %d %s", page.Code, page.Body.String())
	}
	body := page.Body.String()
	seconds := step.RestAfterSeconds
	want := strconv.FormatInt(kept.Progress.Completed[2].CompletedAt.Add(time.Duration(seconds)*time.Second).UnixMilli(), 10)
	if got := regexp.MustCompile(`data-guided-rest="([^"]*)"`).FindStringSubmatch(body); got == nil || got[1] != want {
		t.Errorf("the pause counts against %v, want the deadline %s the recorded set left", got, want)
	}
	if got := regexp.MustCompile(`data-guided-seconds="([^"]*)"`).FindStringSubmatch(body); got == nil || got[1] != strconv.Itoa(int(seconds)) {
		t.Errorf("the pause shows %v seconds, want the %d the step declares", got, seconds)
	}
	if !strings.Contains(body, ">Up next<") || !strings.Contains(body, ">"+kept.Steps[2].Title+"<") {
		t.Errorf("the pause names no next step: %s", at)
	}
	if skip := regexp.MustCompile(`data-guided-skip="".*?href="([^"]*)"`).FindStringSubmatch(body); len(skip) != 2 || skip[1] != fmt.Sprintf("%s/steps/%s/sets/%s", web, kept.Steps[2].ID, kept.Steps[2].Sets[0]) {
		t.Errorf("the pause skips to %q, want %q", skip, fmt.Sprintf("%s/steps/%s/sets/%s", web, kept.Steps[2].ID, kept.Steps[2].Sets[0]))
	}
	longer := guidedCall(h.router, "GET", at+"?extra=15", "", "", "")
	if longer.Code != 200 || !strings.Contains(longer.Body.String(), fmt.Sprintf(`data-guided-seconds="%d"`, seconds+15)) {
		t.Fatalf("fifteen more seconds: %d", longer.Code)
	}
	if !strings.Contains(longer.Body.String(), "extra=30") {
		t.Errorf("the pause that already took fifteen asks for %v, want the next fifteen on top", regexp.MustCompile(`extra=\d+`).FindAllString(longer.Body.String(), -1))
	}
	if got := h.events(t); got != 3 {
		t.Errorf("the pauses recorded %d event(s), want the three sets and nothing of their own", got)
	}
}

// TestTheRestScreenBelongsToThePairThatEarnedIt checks the other half of the same
// address: before the set whose pause it counts is recorded, the screen is not
// this person's, and the session they are actually on answers instead.
func TestTheRestScreenBelongsToThePairThatEarnedIt(t *testing.T) {
	h := newGuided(t)
	mountGuidedScreens(h)
	web := "/app/practice/sessions/" + guidedID.String()
	session := guidedFixture()
	at := fmt.Sprintf("%s/rest/%s/%s", web, session.Steps[1].ID, session.Steps[1].Sets[1])
	recordGuidedSets(t, h, web, 0, 1)

	res := guidedCall(h.router, "GET", at, "", "", "")
	if res.Code != 303 || res.Header().Get("Location") != web {
		t.Fatalf("a pause owed to no one: %d → %s %s", res.Code, res.Header().Get("Location"), res.Body.String())
	}
	if strings.Contains(res.Body.String(), "data-guided-seconds") {
		t.Errorf("the address of a pause not owed still wrote a countdown: %s", at)
	}
}
