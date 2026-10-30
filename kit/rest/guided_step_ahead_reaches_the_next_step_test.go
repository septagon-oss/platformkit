package rest_test

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// guidedPrimaryHref is the address the session screen's one filled control points
// at. The screen that records progress writes that control as a form, so only the
// screens a person is reading — a step behind the recorded prefix or one ahead of
// it — answer with a control that carries an address.
func guidedPrimaryHref(t *testing.T, page string) string {
	t.Helper()
	re := regexp.MustCompile(`data-variant="primary"[^>]*href="([^"]*)"|href="([^"]*)"[^>]*data-variant="primary"`)
	match := re.FindStringSubmatch(page)
	if match == nil {
		t.Fatalf("the screen offers no primary control with an address:\n%s", page)
	}
	for _, group := range match[1:] {
		if group != "" {
			return group
		}
	}
	t.Fatalf("the screen's primary control carries no address:\n%s", page)
	return ""
}

// guidedPosition answers which set of the fixture session an addressed step and
// set pair is, counting from zero, the order a person walks them in.
func guidedPosition(address string) (int, bool) {
	ids := regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`).FindAllString(address, -1)
	if len(ids) < 2 {
		return 0, false
	}
	step, set := uuid.MustParse(ids[len(ids)-2]), uuid.MustParse(ids[len(ids)-1])
	position := 0
	for _, s := range guidedFixture().Steps {
		if s.ID != step {
			position += len(s.Sets)
			continue
		}
		for _, id := range s.Sets {
			if id == set {
				return position, true
			}
			position++
		}
	}
	return 0, false
}

// TestGuidedStepAheadLeadsWhereThePersonMeantToGo follows the control a person uses
// to walk on from a step they are only reading. A step's rest belongs to the set
// that was just recorded, so the screen a person is reading has no rest of its own
// to show them: the address its one primary control carries has to reach a page at
// that step or the next one, rather than bouncing back through the session's resume
// address onto a step the person had already walked away from.
func TestGuidedStepAheadLeadsWhereThePersonMeantToGo(t *testing.T) {
	fixture := newGuided(t)
	mountGuidedScreens(fixture)
	web := "/app/practice/sessions/" + guidedID.String()
	first := guidedInput(0, 1)
	if res := guidedCall(fixture.router, http.MethodPost, web+"/complete-set", url.Values{
		"stepId": {first.StepID.String()}, "setId": {first.SetID.String()}, "ifRevision": {"1"},
	}.Encode(), "application/x-www-form-urlencoded", ""); res.Code != http.StatusSeeOther {
		t.Fatalf("the first set must record before the walk: %d %s", res.Code, res.Body.String())
	}
	// Position 2 is the second set of the step that holds two sets, with a rest
	// declared between them, and it lies ahead of the one recorded set.
	const ahead = 2
	res := guidedCall(fixture.router, http.MethodGet, guidedStepPath(web, ahead), "", "", "")
	if res.Code != http.StatusOK {
		t.Fatalf("the step ahead of the recorded prefix: %d %s", res.Code, res.Body.String())
	}
	href := guidedPrimaryHref(t, res.Body.String())
	walked := []string{href}
	target, code := href, 0
	for hop := 0; hop < 3 && target != ""; hop++ {
		next := guidedCall(fixture.router, http.MethodGet, target, "", "", "")
		code = next.Code
		if code != http.StatusSeeOther {
			break
		}
		target = next.Header().Get("Location")
		walked = append(walked, fmt.Sprintf("%d %s", code, target))
	}
	t.Logf("walk from the step ahead: %v -> %d %s", walked, code, target)
	if code != http.StatusOK {
		t.Errorf("the primary control %s of the step a person is reading never answers with a page: %v", href, walked)
	}
	position, ok := guidedPosition(target)
	if !ok || position < ahead {
		t.Errorf("the primary control %s of the step a person is reading ends the walk on step/set %q (%d), which is not the step they asked for at position %d: %v",
			href, target[strings.LastIndex(target, "/")+1:], position, ahead, walked)
	}
}
