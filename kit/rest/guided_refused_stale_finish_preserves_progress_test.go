package rest_test

import (
	"net/url"
	"strconv"
	"testing"
)

func TestRefusedStaleGuidedFinishPreservesProgress(t *testing.T) {
	h := newGuided(t)
	mountGuidedScreens(h)
	web := "/app/practice/sessions/" + guidedID.String()
	action, held := servedGuidedFinishForm(t, h, web)
	before := h.read(t)
	step, set, ok := guidedFixture().Pair(0)
	if !ok {
		t.Fatal("the fixture walk has no first set")
	}
	body := url.Values{
		"stepId":     {before.Steps[step].ID.String()},
		"setId":      {before.Steps[step].Sets[set].String()},
		"ifRevision": {strconv.FormatInt(before.Progress.Revision, 10)},
	}
	advanced := guidedCall(h.router, "POST", web+"/complete-set", body.Encode(), "application/x-www-form-urlencoded", "")
	if advanced.Code != 303 {
		t.Fatalf("recording the set answered %d", advanced.Code)
	}
	current := h.read(t)
	events := h.events(t)

	refused := guidedCall(h.router, "POST", action, held.Encode(), "application/x-www-form-urlencoded", "")
	if refused.Code < 400 || refused.Header().Get("Location") != "" {
		t.Fatalf("stale finish answered %d with destination %q", refused.Code, refused.Header().Get("Location"))
	}
	after := h.read(t)
	if after.Progress.Revision != current.Progress.Revision || len(after.Progress.Completed) != len(current.Progress.Completed) || after.Progress.Finished != nil {
		t.Fatalf("refused finish changed progress: revision %d, completed %d, finished %v", after.Progress.Revision, len(after.Progress.Completed), after.Progress.Finished != nil)
	}
	if got := h.events(t); got != events {
		t.Fatalf("refused finish emitted %d events, want %d", got, events)
	}
}
