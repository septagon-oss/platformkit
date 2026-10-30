package rest_test

import (
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestRefusedGuidedFinishHasNoStaleResult(t *testing.T) {
	h := newGuided(t)
	mountGuidedScreens(h)
	web := "/app/practice/sessions/" + guidedID.String()
	action, staleForm := servedGuidedFinishForm(t, h, web)
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

	refused := guidedCall(h.router, "POST", action, staleForm.Encode(), "application/x-www-form-urlencoded", "")
	if refused.Code < 400 || refused.Header().Get("Location") != "" {
		t.Fatalf("stale finish answered %d with destination %q", refused.Code, refused.Header().Get("Location"))
	}
	if strings.Contains(refused.Body.String(), guidedFixture().Title) {
		t.Fatal("stale finish returned the session row in its refusal body")
	}
}
