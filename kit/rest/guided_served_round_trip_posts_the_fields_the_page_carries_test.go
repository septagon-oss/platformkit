package rest_test

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// servedForm returns the address and the fields of the page's form that posts to a
// path ending suffix, exactly as the page wrote them: what a person who runs no
// browser script sends back when they press.
func servedForm(t *testing.T, body, suffix string) (string, url.Values) {
	t.Helper()
	for _, m := range regexp.MustCompile(`(?s)<form[^>]*action="([^"]*)"[^>]*>(.*?)</form>`).FindAllStringSubmatch(body, -1) {
		if !strings.HasSuffix(m[1], suffix) {
			continue
		}
		fields := url.Values{}
		for _, f := range regexp.MustCompile(`name="([^"]+)" value="([^"]*)"`).FindAllStringSubmatch(m[2], -1) {
			fields.Set(f[1], f[2])
		}
		return m[1], fields
	}
	t.Fatalf("no page form posting to a path ending %s: %s", suffix, body)
	return "", nil
}

func TestTheServedStepPageRecordsTheSetItsOwnFormCarries(t *testing.T) {
	h := newGuided(t)
	mountGuidedScreens(h)
	web := "/app/practice/sessions/" + guidedID.String()

	step := guidedCall(h.router, "GET", web, "", "", "")
	if step.Code != 303 {
		t.Fatalf("resume answered %d", step.Code)
	}
	page := guidedCall(h.router, "GET", step.Header().Get("Location"), "", "", "")
	if page.Code != 200 {
		t.Fatalf("the step page answered %d", page.Code)
	}
	action, fields := servedForm(t, page.Body.String(), "/complete-set")
	t.Logf("the page offers %s with %v", action, fields)
	res := guidedCall(h.router, "POST", action, fields.Encode(), "application/x-www-form-urlencoded", "")
	if res.Code != 303 {
		t.Errorf("the completion fields the page itself carries, posted unchanged, answered %d: %s", res.Code, res.Body.String())
	}
	if h.events(t) != 1 {
		t.Errorf("the press wrote %d events, want the one recorded set", h.events(t))
	}

	next := guidedCall(h.router, "GET", web, "", "", "")
	if next.Code != 303 {
		t.Fatalf("the walk after the press answered %d", next.Code)
	}
	t.Logf("resume now leads to %s", next.Header().Get("Location"))
}

func TestTheServedFinishPageEndsTheWalkOnTheRevisionItCarries(t *testing.T) {
	h := newGuided(t)
	mountGuidedScreens(h)
	web := "/app/practice/sessions/" + guidedID.String()
	for _, position := range []int{0, 1, 2, 3} {
		in := guidedInput(position, int64(position+1))
		body := url.Values{"stepId": {in.StepID.String()}, "setId": {in.SetID.String()}, "ifRevision": {strconv.FormatInt(in.IfRevision, 10)}}.Encode()
		if res := guidedCall(h.router, "POST", web+"/complete-set", body, "application/x-www-form-urlencoded", ""); res.Code != 303 {
			t.Fatalf("recording set %d answered %d", position, res.Code)
		}
	}
	page := guidedCall(h.router, "GET", web, "", "", "")
	if page.Code != 303 {
		t.Fatalf("a walk with every set recorded answered %d on resume", page.Code)
	}
	at := page.Header().Get("Location")
	if !strings.HasSuffix(at, "/finish") {
		t.Fatalf("a walk with every set recorded resumes to %s, not its finish", at)
	}
	finish := guidedCall(h.router, "GET", at, "", "", "")
	if finish.Code != 200 {
		t.Fatalf("the finish page answered %d", finish.Code)
	}
	action, fields := servedForm(t, finish.Body.String(), "/finish")
	t.Logf("the finish page offers %s with %v", action, fields)
	res := guidedCall(h.router, "POST", action, fields.Encode(), "application/x-www-form-urlencoded", "")
	if res.Code != 303 {
		t.Errorf("the finish fields the page itself carries, posted unchanged, answered %d: %s", res.Code, res.Body.String())
	}
}
