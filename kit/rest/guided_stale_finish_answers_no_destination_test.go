package rest_test

import (
	"net/url"
	"strconv"
	"testing"
)

func TestAStaleGuidedFinishAnswersNoDestination(t *testing.T) {
	h := newGuided(t)
	mountGuidedScreens(h)
	web := "/app/practice/sessions/" + guidedID.String()
	action, held := servedGuidedFinishForm(t, h, web)
	first := h.read(t)

	step, set, ok := guidedFixture().Pair(0)
	if !ok {
		t.Fatal("the fixture walk has no first set")
	}
	body := url.Values{
		"stepId":     {first.Steps[step].ID.String()},
		"setId":      {first.Steps[step].Sets[set].String()},
		"ifRevision": {strconv.FormatInt(first.Progress.Revision, 10)},
	}
	advanced := guidedCall(h.router, "POST", web+"/complete-set", body.Encode(), "application/x-www-form-urlencoded", "")
	if advanced.Code != 303 {
		t.Fatalf("recording the set answered %d", advanced.Code)
	}

	refused := guidedCall(h.router, "POST", action, held.Encode(), "application/x-www-form-urlencoded", "")
	if refused.Code < 400 {
		t.Fatalf("the stale finish form answered %d, want a refusal", refused.Code)
	}
	if location := refused.Header().Get("Location"); location != "" {
		t.Fatalf("the stale finish form answered with destination %q", location)
	}
}
