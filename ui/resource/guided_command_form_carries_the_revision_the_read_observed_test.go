package resource_test

// A guided form is a retraction-proof promise: the command it posts rechecks the
// revision it carries and refuses the write where the walk moved on. So the number
// hidden in the form is not decoration — it is the answer the page observed when it
// was read, and the only number a person can send back without a browser script.
// Nothing reads that number off the page today: the guided cases post a revision they
// worked out themselves, so a page could write a completed count, a stale revision or
// nothing at all into its own form and every case would stay green while a person who
// runs no script is refused each time they press. This asks both command forms of one
// walk to carry the revision that walk's own read reported.

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
	"golang.org/x/net/html"
)

// carriedWords is the guided vocabulary of a walk that records: the step page's own
// words, and the two endings the finish page keeps apart.
func carriedWords() map[string]string {
	return map[string]string{
		"step": "Step {current} of {total}", "set": "Set {current} of {total}",
		"completed_step": "Step {current} of {total}, completed",
		"end":            "End of session", "completed": "Completed {completed} of {total} sets",
		"done_set": "Done set", "next_step": "Next step", "next_set": "Next set",
		"finish": "Finish session", "finish_partial": "Finish with {completed} of {total} sets",
		"continue": "Continue", "previous_step": "Previous step",
		"exit": "Exit session", "timers": "Timers",
		"no_script": "Live countdown and sound need JavaScript.",
		"keyboard":  "Arrow keys change steps. Space starts a timer or skips rest.",
	}
}

// carriedWalk is a walk of two steps and three sets whose first set is recorded: the
// state of a person midway through it. Its read reports revision two, one behind the
// three sets it names, which is why a form that counts sets instead of carrying that
// revision sends a number the command will not accept.
func carriedWalk() entity.GuidedSession {
	id, first, second, last := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	step := func(name string, sets ...uuid.UUID) entity.GuidedStep {
		return entity.GuidedStep{ID: uuid.New(), Title: name, Text: "Follow the instruction.", Sets: sets}
	}
	steps := []entity.GuidedStep{step("Prepare", first, second), step("Stretch", last)}
	receipt := entity.GuidedCompletion{SessionID: id, StepID: steps[0].ID, SetID: first, Revision: 2,
		CompletedAt: time.Unix(1777000000, 0).UTC()}
	return entity.GuidedSession{ID: id, Title: "Practice sequence", Steps: steps,
		Progress: &entity.GuidedProgress{Revision: 2, Completed: []entity.GuidedCompletion{receipt}}}
}

// carriedPage renders one page of a walk that records, with both of its commands
// declared and callable, as the mount that offers them would ask for it.
func carriedPage(t *testing.T, session entity.GuidedSession, mode string, step, set int) string {
	t.Helper()
	view := resource.Guided(resource.GuidedPage{
		Session: session,
		Policy: resource.GuidedPolicy{
			Complete: resource.GuidedCapability{Declared: true, Callable: true},
			Finish:   resource.GuidedCapability{Declared: true, Callable: true},
			Labels:   carriedWords()},
		Base: "/app/practice/sessions/" + session.ID.String(),
		Exit: "/app", Mode: mode, Step: step, Set: set,
	}, resource.Options{})
	var out strings.Builder
	for _, node := range view.Body {
		if err := node.Render(&out); err != nil {
			t.Fatalf("the %s page of a walk that records cannot be rendered: %v", mode, err)
		}
	}
	return out.String()
}

// carriedForm returns the hidden values of the form on a page that posts to one
// address; a page offering no such form carries none, and the map is empty.
func carriedForm(t *testing.T, page, action string) map[string]string {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatalf("the page is no document this case can read: %v", err)
	}
	fields := map[string]string{}
	for n := range doc.Descendants() {
		if n.Type != html.ElementNode || n.Data != "form" || carriedAttr(n, "action") != action {
			continue
		}
		for f := range n.Descendants() {
			if f.Type == html.ElementNode && f.Data == "input" {
				fields[carriedAttr(f, "name")] = carriedAttr(f, "value")
			}
		}
	}
	return fields
}

// carriedAttr is one attribute of an element, or nothing where it carries none.
func carriedAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// TestEachGuidedCommandFormCarriesTheRevisionTheReadObserved renders the two pages of
// one walk that record — the step page whose control records the next set, and the
// finish page whose control ends the walk — and asks each form for the revision the
// session's read reported. A form carrying any other number sends the person's own
// press back to them as a refusal, because the command rechecks the revision it is
// given against the one the walk stands on.
func TestEachGuidedCommandFormCarriesTheRevisionTheReadObserved(t *testing.T) {
	session := carriedWalk()
	if err := session.Validate(true); err != nil {
		t.Fatalf("the walk this case reads is no read a module could answer with: %v", err)
	}
	const observed = "2"
	base := "/app/practice/sessions/" + session.ID.String()

	stepForm := carriedForm(t, carriedPage(t, session, "step", 0, 1), base+"/complete-set")
	if got := stepForm["ifRevision"]; got != observed {
		t.Errorf("the completion form of a walk whose read reported revision %s carries %q, so the press that records the next set is refused by the command that would take it: %v", observed, got, stepForm)
	}
	if stepForm["stepId"] == "" || stepForm["setId"] == "" {
		t.Errorf("the completion form carries no pair of its own (%v), so the command cannot know which set the person recorded", stepForm)
	}

	finishForm := carriedForm(t, carriedPage(t, session, "finish", 0, 0), base+"/finish")
	if got := finishForm["ifRevision"]; got != observed {
		t.Errorf("the finish form of a walk whose read reported revision %s carries %q, so a person who runs no script cannot end the walk they finished: %s", observed, got, base+"/finish")
	}
}
