package rest_test

// The pause between sets is the one guided screen a person who never runs a
// script still has to read: it says how long to wait. So the page carries that
// second twice — an instant the browser counts against, and the same instant in
// words for the page that never counts — and the seconds it offers on top stop
// where a day stops, so the screen never promises a pause it cannot show.

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// completeGuidedPair records one pair of the fixture session through the same
// native form a person without JavaScript sends.
func completeGuidedPair(t *testing.T, h guidedHarness, web string, position int) {
	t.Helper()
	in := guidedInput(position, int64(position+1))
	form := url.Values{"stepId": {in.StepID.String()}, "setId": {in.SetID.String()}, "ifRevision": {fmt.Sprint(in.IfRevision)}}.Encode()
	if res := guidedCall(h.router, "POST", web+"/complete-set", form, "application/x-www-form-urlencoded", ""); res.Code != 303 {
		t.Fatalf("pair %d: %d %s", position, res.Code, res.Body.String())
	}
}

// guidedRestPage opens the pause after the last recorded pair of the fixture
// session and returns it with the address it was asked for.
func guidedRestPage(t *testing.T, h guidedHarness, web string, query string) (body, at string) {
	t.Helper()
	kept := h.read(t)
	if len(kept.Progress.Completed) == 0 {
		t.Fatal("no pair is recorded, so no pause is owed")
	}
	last := kept.Progress.Completed[len(kept.Progress.Completed)-1]
	step, set := kept.Steps[1], kept.Steps[1].Sets[1]
	if last.StepID != step.ID || last.SetID != set {
		t.Fatalf("the pause asked for belongs to %s/%s, the session recorded %s/%s", step.ID, set, last.StepID, last.SetID)
	}
	at = fmt.Sprintf("%s/rest/%s/%s%s", web, step.ID, set, query)
	res := guidedCall(h.router, "GET", at, "", "", "")
	if res.Code != 200 {
		t.Fatalf("%s: %d %s", at, res.Code, res.Body.String())
	}
	return res.Body.String(), at
}

// TestTheRestPageWithoutAScriptSaysTheSecondTheCountdownRunsTo checks that the
// instant the browser counts against and the moment the page itself prints are
// one moment: the written line is the deadline in the attribute, so a person who
// runs no script waits the same pause rather than one they worked out themselves.
func TestTheRestPageWithoutAScriptSaysTheSecondTheCountdownRunsTo(t *testing.T) {
	h := newGuided(t)
	mountGuidedScreens(h)
	web := "/app/practice/sessions/" + guidedID.String()
	for _, position := range []int{0, 1, 2} {
		completeGuidedPair(t, h, web, position)
	}
	body, at := guidedRestPage(t, h, web, "")

	got := regexp.MustCompile(`data-guided-rest="([^"]*)"`).FindStringSubmatch(body)
	if got == nil {
		t.Fatalf("%s writes no deadline to count against", at)
	}
	millis, err := strconv.ParseInt(got[1], 10, 64)
	if err != nil {
		t.Fatalf("%s counts against %q, which is no instant: %v", at, got[1], err)
	}
	want := time.UnixMilli(millis).UTC().Format(time.RFC3339)
	found := false
	for _, said := range regexp.MustCompile(`(?s)<noscript>(.*?)</noscript>`).FindAllStringSubmatch(body, -1) {
		found = found || strings.Contains(said[1], want)
	}
	if !found {
		t.Errorf("%s writes line(s) for a page that runs no script, none of them the %s the countdown runs to", at, want)
	}
	if written := h.events(t); written != 3 {
		t.Errorf("the pause wrote %d event(s), want the three recorded pairs and nothing of its own", written)
	}
}

// TestTheRestPageStopsAddingSecondsAtADay checks the ceiling of the same screen:
// the pause a person asks for on top of the declared one never runs past a day,
// and the link that offers the next fifteen stops at that ceiling rather than
// climbing past it. The ask is answered either by a countdown within a day or by
// a refusal of the number; what it is never answered by is a longer pause.
func TestTheRestPageStopsAddingSecondsAtADay(t *testing.T) {
	h := newGuided(t)
	mountGuidedScreens(h)
	web := "/app/practice/sessions/" + guidedID.String()
	for _, position := range []int{0, 1, 2} {
		completeGuidedPair(t, h, web, position)
	}
	res := guidedCall(h.router, "GET", web+"/rest/"+guidedFixture().Steps[1].ID.String()+"/"+guidedFixture().Steps[1].Sets[1].String()+"?extra=86400", "", "", "")
	if res.Code == 422 {
		return
	}
	if res.Code != 200 {
		t.Fatalf("a day of seconds: %d %s", res.Code, res.Body.String())
	}
	body := res.Body.String()
	shown := regexp.MustCompile(`data-guided-seconds="([^"]*)"`).FindStringSubmatch(body)
	if shown == nil {
		t.Fatalf("no seconds to count: %s", body)
	}
	seconds, err := strconv.Atoi(shown[1])
	if err != nil || seconds > 86400 {
		t.Errorf("the pause offers %s, want no more than the 86400 seconds of a day", shown[1])
	}
	if seconds != 86400 {
		t.Logf("the pause was held at %d seconds rather than the day asked", seconds)
	}
	offer := regexp.MustCompile(`data-guided-add=""[^>]*href="([^"]*)"`).FindStringSubmatch(body)
	if offer == nil {
		t.Fatalf("the pause offers no seconds on top: %s", body)
	}
	extra := regexp.MustCompile(`extra=(\d+)`).FindStringSubmatch(offer[1])
	if extra == nil {
		t.Fatalf("%q offers no number of seconds: %s", offer[1], body)
	}
	declared := int(guidedFixture().Steps[1].RestAfterSeconds)
	if again, err := strconv.Atoi(extra[1]); err != nil || again+declared > 86400 {
		t.Errorf("the pause that is already running to %d seconds asks for %s more, want a pause within a day", seconds, extra[1])
	}
}
