package resource_test

// A person who runs no browser script presses the form the page holds, so the pair
// hidden in that form is the whole of their order: the command records the set it is
// given, keyed by session, step and set. A form that named the step's first set from
// its own position rather than the pair the page stands on would send the same set
// back twice — the second press is the idempotent answer to the first, so the walk
// never moves on, and no case that builds its own request body would notice. This
// renders the pages of one walk whose set counts differ and asks each form for the
// pair under the person's finger, which is the pair the page says it shows.

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// pairWords is the guided vocabulary a walk that records is rendered with.
func pairWords() map[string]string {
	return map[string]string{
		"step": "Step {current} of {total}", "set": "Set {current} of {total}",
		"end": "End of session", "completed": "Completed {completed} of {total} sets",
		"done_set": "Done set", "next_step": "Next step", "next_set": "Next set",
		"finish": "Finish session", "continue": "Continue",
		"previous_step": "Previous step", "exit": "Exit session", "timers": "Timers",
	}
}

// pairWalk is a walk whose first step, named by stepOne and holding setOne and
// setTwo, is followed by a step of one set of its own. The leading recorded sets are
// the ones a person standing further along the walk has already pressed, and the
// revision counts them the way a read of that walk answers.
func pairWalk(recorded int, stepOne, setOne, setTwo, session uuid.UUID) entity.GuidedSession {
	prepare := entity.GuidedStep{ID: stepOne, Title: "Prepare", Text: "Follow the instruction.", Sets: []uuid.UUID{setOne, setTwo}}
	stretch := entity.GuidedStep{ID: uuid.New(), Title: "Stretch", Text: "Follow the instruction.", Sets: []uuid.UUID{uuid.New()}}
	steps := []entity.GuidedStep{prepare, stretch}
	pressed := []struct{ step, set uuid.UUID }{{prepare.ID, setOne}, {prepare.ID, setTwo}, {stretch.ID, stretch.Sets[0]}}
	receipts := make([]entity.GuidedCompletion, 0, recorded)
	for i := range recorded {
		receipts = append(receipts, entity.GuidedCompletion{SessionID: session, StepID: pressed[i].step, SetID: pressed[i].set,
			Revision: int64(i + 2), CompletedAt: time.Unix(1777000000, 0).UTC()})
	}
	return entity.GuidedSession{ID: session, Title: "Practice sequence", Steps: steps,
		Progress: &entity.GuidedProgress{Revision: int64(recorded + 1), Completed: receipts}}
}

// pairPage renders one step page of a walk that records, with both of its commands
// declared and callable, as the mount that offers them would ask for it.
func pairPage(t *testing.T, session entity.GuidedSession, step, set int) string {
	t.Helper()
	view := resource.Guided(resource.GuidedPage{
		Session: session,
		Policy: resource.GuidedPolicy{
			Complete: resource.GuidedCapability{Declared: true, Callable: true},
			Finish:   resource.GuidedCapability{Declared: true, Callable: true},
			Labels:   pairWords()},
		Base: "/app/practice/sessions/" + session.ID.String(),
		Exit: "/app", Mode: "step", Step: step, Set: set,
	}, resource.Options{})
	var out strings.Builder
	for _, node := range view.Body {
		if err := node.Render(&out); err != nil {
			t.Fatalf("the step page of a walk that records cannot be rendered: %v", err)
		}
	}
	return out.String()
}

// pairHidden returns one hidden field of the completion form a page posts to at, and
// refuses the page where it posts no such form or carries no such field.
func pairHidden(t *testing.T, page, name, at string) string {
	t.Helper()
	if !strings.Contains(page, `action="`+at+`"`) {
		t.Fatalf("the page offers no form posting to %s: %s", at, page)
	}
	field := regexp.MustCompile(`name="` + name + `" value="([^"]*)"`).FindStringSubmatch(page)
	if field == nil {
		t.Fatalf("the form posting to %s carries no %s: %s", at, name, page)
	}
	return field[1]
}

// TestEachGuidedCommandFormCarriesThePairThePageStandsOn renders the second set of a
// two-set step and the first set of the step after it, and asks each page's completion
// form for the step and set the page shows. The form has to carry that same pair,
// because it is the set the person's press records.
func TestEachGuidedCommandFormCarriesThePairThePageStandsOn(t *testing.T) {
	sessionID, stepOne, setOne, setTwo := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	action := "/app/practice/sessions/" + sessionID.String() + "/complete-set"

	mid := pairWalk(1, stepOne, setOne, setTwo, sessionID)
	if err := mid.Validate(true); err != nil {
		t.Fatalf("the walk midway through its first step is no read a module could answer with: %v", err)
	}
	page := pairPage(t, mid, 0, 1)
	if !strings.Contains(page, "Set 2 of 2") {
		t.Fatalf("the page of a walk's second set says nothing about standing on it: %s", page)
	}
	if got := pairHidden(t, page, "stepId", action); got != stepOne.String() {
		t.Errorf("the form of the page showing \"Set 2 of 2\" names step %q, not %s, so the press records a set of another step", got, stepOne)
	}
	if got := pairHidden(t, page, "setId", action); got != setTwo.String() {
		t.Errorf("the form of the page showing \"Set 2 of 2\" names set %q, not the %s the person is on, so the press repeats the set already recorded", got, setTwo)
	}

	onNext := pairWalk(2, stepOne, setOne, setTwo, sessionID)
	if err := onNext.Validate(true); err != nil {
		t.Fatalf("the walk standing on its second step is no read a module could answer with: %v", err)
	}
	page = pairPage(t, onNext, 1, 0)
	if !strings.Contains(page, "Step 2 of 2") {
		t.Fatalf("the page of a walk's second step says nothing about standing on it: %s", page)
	}
	if got := pairHidden(t, page, "stepId", action); got != onNext.Steps[1].ID.String() {
		t.Errorf("the form of the page showing \"Step 2 of 2\" names step %q, not %s, so the press records a set of the step the person left", got, onNext.Steps[1].ID)
	}
	if got := pairHidden(t, page, "setId", action); got != onNext.Steps[1].Sets[0].String() {
		t.Errorf("the form of the page showing \"Step 2 of 2\" names set %q, not the %s it stands on", got, onNext.Steps[1].Sets[0])
	}
}
