package rest_test

import (
	"net/url"
	"strconv"
	"testing"
)

func TestStaleGuidedFinishKeepsExactCompletedPairs(t *testing.T) {
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
	want := h.read(t).Progress

	refused := guidedCall(h.router, "POST", action, held.Encode(), "application/x-www-form-urlencoded", "")
	if refused.Code < 400 || refused.Header().Get("Location") != "" {
		t.Fatalf("stale finish answered %d with destination %q", refused.Code, refused.Header().Get("Location"))
	}
	got := h.read(t).Progress
	if got.Revision != want.Revision || got.Finished != nil || len(got.Completed) != len(want.Completed) {
		t.Fatalf("stale finish changed progress: got revision %d, completed %d, finished %v; want revision %d, completed %d, unfinished", got.Revision, len(got.Completed), got.Finished != nil, want.Revision, len(want.Completed))
	}
	for i := range want.Completed {
		if got.Completed[i] != want.Completed[i] {
			t.Fatalf("stale finish changed completed pair %d: got %+v, want %+v", i, got.Completed[i], want.Completed[i])
		}
	}
}
