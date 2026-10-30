package resource_test

// A guided read that records nothing carries no progress: the session it returns names
// its steps and nothing about where the person stands, because the module behind it
// declares neither the completion command nor the finish one. Every control on the
// pages such a read produces is therefore a link — walking, not recording — and the
// form that would post a command has no revision to carry, because the number it would
// carry comes from the progress that is absent. This asks both pages a recording read
// never reaches to say what they offer a person whose session records nothing: that the
// step page moves them along in its own words, that the finish page says nothing was
// recorded, and that neither carries a form whose command nobody declared.

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
	"golang.org/x/net/html"
)

// unrecordedWords is the guided vocabulary of a mount that declares a session read and
// no command beside it: every line the step page and the finish page speak.
func unrecordedWords() map[string]string {
	return map[string]string{
		"step": "Step {current} of {total}", "set": "Set {current} of {total}",
		"completed_step": "Step {current} of {total}, completed",
		"end":            "End of session", "completed": "Completed {completed} of {total} sets",
		"nothing_recorded":       "Nothing recorded",
		"completion_unavailable": "Set completion is unavailable.",
		"navigation_only":        "Next does not record progress.",
		"previous_step":          "Previous step", "next_step": "Next step", "next_set": "Next set",
		"exit": "Exit session", "finish": "Finish session", "continue": "Continue",
		"timers": "Timers", "no_script": "Live countdown and sound need JavaScript.",
		"keyboard": "Arrow keys change steps. Space starts a timer or skips rest.",
	}
}

// unrecordedSession is a session of two steps and three sets that carries no progress at
// all, which is the shape a read with no command answers with.
func unrecordedSession() entity.GuidedSession {
	step := func(name string, sets ...uuid.UUID) entity.GuidedStep {
		return entity.GuidedStep{ID: uuid.New(), Title: name, Text: "Follow the instruction.", Sets: sets}
	}
	return entity.GuidedSession{ID: uuid.New(), Title: "Practice sequence",
		Steps: []entity.GuidedStep{step("Prepare", uuid.New(), uuid.New()), step("Stretch", uuid.New())}}
}

// unrecordedPage renders one page of a session that records nothing, as the mount that
// declares no command would ask for it: neither capability declared, so none callable.
func unrecordedPage(t *testing.T, session entity.GuidedSession, mode string, step, set int) string {
	t.Helper()
	view := resource.Guided(resource.GuidedPage{
		Session: session,
		Policy:  resource.GuidedPolicy{Labels: unrecordedWords()},
		Base:    "/app/practice/sessions/" + session.ID.String(),
		Exit:    "/app",
		Mode:    mode,
		Step:    step,
		Set:     set,
	}, resource.Options{})
	var out strings.Builder
	for _, node := range view.Body {
		if err := node.Render(&out); err != nil {
			t.Fatalf("the %s page of a session that records nothing cannot be rendered: %v", mode, err)
		}
	}
	return strings.Join(strings.Fields(out.String()), " ")
}

// postedVerbs names the command each form on a rendered page posts, by the address it
// posts to. A page with no form on it names none.
func postedVerbs(t *testing.T, page, base string) []string {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatalf("the page is no document this case can read: %v", err)
	}
	var verbs []string
	for n := range doc.Descendants() {
		if n.Type != html.ElementNode || n.Data != "form" {
			continue
		}
		action := ""
		for _, a := range n.Attr {
			if a.Key == "action" {
				action = a.Val
			}
		}
		verbs = append(verbs, strings.TrimPrefix(action, base+"/"))
	}
	return verbs
}

// TestAGuidedPageOfAReadThatRecordsNothingOffersNoFormToPress walks both pages a session
// read with no command produces. The step page must still say which step it shows and
// that walking records nothing, and the finish page must still say that nothing was
// recorded — those words are the page being read, not a control being assumed — and
// neither page may carry a form: the command it would post is undeclared, and the
// revision it would carry belongs to a progress this read does not have. A step page
// that posted a completion here would record a set the person was only walking past, and
// a finish page that posted a finish would end a walk nobody asked to end.
func TestAGuidedPageOfAReadThatRecordsNothingOffersNoFormToPress(t *testing.T) {
	session := unrecordedSession()
	base := "/app/practice/sessions/" + session.ID.String()

	stepPage := unrecordedPage(t, session, "step", 0, 0)
	if !strings.Contains(stepPage, "Step 1 of 2") {
		t.Errorf("the step page of a session that records nothing does not say which step it shows, so this is not the page the case came to read: %s", stepPage)
	}
	if !strings.Contains(stepPage, "Next does not record progress.") {
		t.Errorf("the step page of a session that records nothing does not say that walking records nothing, which is the only truth its controls can tell: %s", stepPage)
	}
	if verbs := postedVerbs(t, stepPage, base); len(verbs) != 0 {
		t.Errorf("the step page of a session that declares no completion command offers a form posting %v, which would record a set the person was only walking past", verbs)
	}

	finishPage := unrecordedPage(t, session, "finish", 0, 0)
	if !strings.Contains(finishPage, "Nothing recorded") {
		t.Errorf("the finish page of a session that records nothing does not say nothing was recorded, so the person cannot tell an unwalked session from a finished one: %s", finishPage)
	}
	if verbs := postedVerbs(t, finishPage, base); len(verbs) != 0 {
		t.Errorf("the finish page of a session that declares no finish command offers a form posting %v, which would end a walk nobody asked to end and carries a revision this read does not have", verbs)
	}
}
