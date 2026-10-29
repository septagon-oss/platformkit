package rest_test

import (
	"net/url"
	"strconv"
	"testing"
)

func TestRefusedStaleGuidedFinishKeepsRecordedPairs(t *testing.T) {
	h := newGuided(t)
	mountGuidedScreens(h)
	web := "/app/practice/sessions/" + guidedID.String()
	action, staleForm := servedGuidedFinishForm(t, h, web)
	before := h.read(t).Progress
	step, set, ok := guidedFixture().Pair(0)
	if !ok {
		t.Fatal("the fixture walk has no first set")
	}
	body := url.Values{
		"stepId":     {guidedFixture().Steps[step].ID.String()},
		"setId":      {guidedFixture().Steps[step].Sets[set].String()},
		"ifRevision": {strconv.FormatInt(before.Revision, 10)},
	}
	advanced := guidedCall(h.router, "POST", web+"/complete-set", body.Encode(), "application/x-www-form-urlencoded", "")
	if advanced.Code != 303 {
		t.Fatalf("recording the set answered %d", advanced.Code)
	}
	want := h.read(t).Progress
	refused := guidedCall(h.router, "POST", action, staleForm.Encode(), "application/x-www-form-urlencoded", "")
	if refused.Code < 400 || refused.Header().Get("Location") != "" {
		t.Fatalf("stale finish answered %d with destination %q", refused.Code, refused.Header().Get("Location"))
	}
	got := h.read(t).Progress
	if got.Revision != want.Revision || got.Finished != nil || len(got.Completed) != len(want.Completed) {
		t.Fatalf("refused stale finish changed progress: got revision %d and %d completed pairs; want revision %d and %d unfinished pairs", got.Revision, len(got.Completed), want.Revision, len(want.Completed))
	}
	for i := range want.Completed {
		if got.Completed[i] != want.Completed[i] {
			t.Fatalf("refused stale finish changed completed pair %d: got %+v, want %+v", i, got.Completed[i], want.Completed[i])
		}
	}
}
