package rest_test

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// guidedPageText is the copy a guided screen shows a person: the spans the page
// layer writes its labels and content into, with the scripts that carry the
// enhancement's own vocabulary stripped, because those name every word the
// screen could use on a browser that runs them and say nothing about the page.
var (
	guidedScript  = regexp.MustCompile(`(?s)<script.*?</script>`)
	guidedTag     = regexp.MustCompile(`<[^>]*>`)
	guidedSpacing = regexp.MustCompile(`\s+`)
	guidedEntity  = strings.NewReplacer("&#34;", `"`, "&#39;", "'", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&nbsp;", " ")
)

// guidedStepPath is the address of one step and set of the session the fixture
// holds, the way a browser is pointed at it.
func guidedStepPath(web string, position int) string {
	s := guidedFixture()
	step, set, ok := s.Pair(position)
	if !ok {
		panic("the fixture does not reach that position")
	}
	return fmt.Sprintf("%s/steps/%s/sets/%s", web, s.Steps[step].ID, s.Steps[step].Sets[set])
}

func guidedPageText(t *testing.T, body string) string {
	t.Helper()
	html := guidedScript.ReplaceAllString(body, " ")
	html = guidedEntity.Replace(html)
	text := guidedTag.ReplaceAllString(html, " ")
	return strings.TrimSpace(guidedSpacing.ReplaceAllString(text, " "))
}

// TestGuidedStepAwayFromTheRecordedOneWalksOn checks the screen a person reaches
// with the arrow keys when the step under them is not the next one to record:
// back to re-read a finished step, or ahead to see what comes next. Neither step
// has a set the person may record from where they stand, so the screen navigates
// and says nothing about completion, which nobody attempted.
func TestGuidedStepAwayFromTheRecordedOneWalksOn(t *testing.T) {
	fixture := newGuided(t)
	mountGuidedScreens(fixture)
	web := "/app/practice/sessions/" + guidedID.String()

	first := guidedInput(0, 1)
	body := url.Values{
		"stepId": {first.StepID.String()}, "setId": {first.SetID.String()}, "ifRevision": {"1"},
	}.Encode()
	if res := guidedCall(fixture.router, http.MethodPost, web+"/complete-set", body,
		"application/x-www-form-urlencoded", ""); res.Code != http.StatusSeeOther {
		t.Fatalf("the first set must record before the walk: %d %s", res.Code, res.Body.String())
	}

	// Step 1 holds the set that is already recorded; step 3 is two ahead of the
	// recorded prefix. Neither is the set a completion command would take now.
	for _, at := range []struct {
		name    string
		target  string
		wayBack string
	}{
		{"a step whose set is already recorded", guidedStepPath(web, 0), "Next step"},
		{"a step ahead of the recorded prefix", guidedStepPath(web, 3), "Next step"},
	} {
		res := guidedCall(fixture.router, http.MethodGet, at.target, "", "", "")
		if res.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", at.name, res.Code, res.Body.String())
		}
		text := guidedPageText(t, res.Body.String())
		if !strings.Contains(text, at.wayBack) && !strings.Contains(text, "Next set") {
			t.Errorf("%s offers no %q or %q to walk on with: %s", at.name, at.wayBack, "Next set", text)
		}
		if strings.Contains(text, "Set completion is unavailable.") {
			t.Errorf("%s is worded as a refused command: %s", at.name, text)
		}
	}
}
