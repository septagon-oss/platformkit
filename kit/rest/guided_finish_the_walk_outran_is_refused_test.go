package rest_test

// A person who runs no browser script goes where the answer to their press says, and
// a press that was refused says nothing about where to go: nothing was written, so no
// address is owed. The refusal matters most at the end of a walk, because the address
// a finish answers with is the summary, and a person sent there reads the walk as
// ended. This is reachable in an ordinary shape of the page surface: the finish page
// stays open on one screen — every step page links to it — while progress is recorded
// on another, so the held form now quotes a revision the walk has left behind. Every
// case that presses a guided page presses the page it just fetched against the
// revision the walk currently carries, so the answer a refused finish gives has never
// been read here. A refused finish that answered with the summary would leave the
// person on a page for a walk that ended, holding a walk that is still running.

import (
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// servedGuidedFinishForm asks the finish page of one walk — the address every step
// page's own "finish" control names — and returns the action and the fields of the
// form it carries, priced against the revision the walk carries right now.
func servedGuidedFinishForm(t *testing.T, h guidedHarness, web string) (string, url.Values) {
	t.Helper()
	page := guidedCall(h.router, "GET", web+"/finish", "", "", "")
	if page.Code != 200 {
		t.Fatalf("the finish page of a walk still under way answered %d", page.Code)
	}
	return servedForm(t, page.Body.String(), "/finish")
}

// TestAGuidedFinishTheWalkOutranIsRefusedAndAnswersNoAddress records one set, asks the
// finish page for its form, records a second set, and then presses that held form. The
// walk has moved on past the revision the form quotes, so the press is refused: it
// answers no address at all, the walk holds the two sets and the revision the second
// press left, no summary is held and no further event is emitted, and the summary the
// refused press must not hand out stays no page.
func TestAGuidedFinishTheWalkOutranIsRefusedAndAnswersNoAddress(t *testing.T) {
	h := newGuided(t)
	mountGuidedScreens(h)
	web := "/app/practice/sessions/" + guidedID.String()

	action, held := servedGuidedFinishForm(t, h, web)
	first := h.read(t)
	if len(first.Progress.Completed) != 0 || h.events(t) != 0 {
		t.Fatalf("a fresh walk holds %d recorded sets and %d events, want none of either", len(first.Progress.Completed), h.events(t))
	}
	if held.Get("ifRevision") != strconv.FormatInt(first.Progress.Revision, 10) {
		t.Fatalf("the finish form does not quote the revision of the walk it was served for: %q against %d", held.Get("ifRevision"), first.Progress.Revision)
	}

	for position := range 2 {
		step, set, ok := guidedFixture().Pair(position)
		if !ok {
			t.Fatalf("the fixture walk holds no pair at position %d", position)
		}
		body := url.Values{"stepId": {first.Steps[step].ID.String()}, "setId": {first.Steps[step].Sets[set].String()},
			"ifRevision": {strconv.FormatInt(first.Progress.Revision+int64(position), 10)}}
		res := guidedCall(h.router, "POST", web+"/complete-set", body.Encode(), "application/x-www-form-urlencoded", "")
		if res.Code != 303 {
			t.Fatalf("the set recorded while the finish page was held answered %d", res.Code)
		}
	}
	moved := h.read(t)
	if len(moved.Progress.Completed) != 2 || h.events(t) != 2 {
		t.Fatalf("the walk the held page quotes moved to %d sets and %d events, want two of each", len(moved.Progress.Completed), h.events(t))
	}

	res := guidedCall(h.router, "POST", action, held.Encode(), "application/x-www-form-urlencoded", "")
	if res.Code < 400 {
		t.Errorf("the press of a finish form the walk outran answered %d, so the person reads the walk as ended; want a refusal at 400 or above", res.Code)
	}
	if at := res.Header().Get("Location"); at != "" {
		t.Errorf("the refused finish press answers with the address %s to go to, so a walk that is still running sends its person to the summary of a walk that ended", at)
	}
	if said := strings.ToLower(res.Body.String()); !strings.Contains(said, "conflict") && !strings.Contains(said, "refus") && !strings.Contains(said, "stale") {
		t.Errorf("the refused finish press answers with nothing that says the walk did not end: %q", res.Body.String())
	}

	after := h.read(t)
	if len(after.Progress.Completed) != 2 || after.Progress.Revision != moved.Progress.Revision {
		t.Errorf("the refused finish press left %d recorded sets at revision %d, want the two sets the walk already held at revision %d",
			len(after.Progress.Completed), after.Progress.Revision, moved.Progress.Revision)
	}
	if after.Progress.Finished != nil {
		t.Errorf("the refused finish press left a summary on a walk that is still running: %+v", after.Progress.Finished)
	}
	if got := h.events(t); got != 2 {
		t.Errorf("the refused finish press emitted %d events, want the two the recorded sets emitted", got)
	}
	if summary := guidedCall(h.router, "GET", web+"/summary", "", "", ""); summary.Code == 200 {
		t.Errorf("the summary answers as a page for a walk that never ended, so a person who only pressed a stale form reads an ending")
	}
}
