package screens_test

// A guided walk ends in two steps, not one: recording the last set does not end it, the
// finish does. So a session whose every set is recorded, with no summary returned yet, has
// nowhere left to walk to, and the session address has to send the person to the finish page.
// The pair a walk resumes to is found by counting recorded sets, and a count one past the
// last set names no pair — read as a pair anyway it names the first set of the first step, so
// a walk that lost that boundary would hand a person who had recorded everything the set they
// started on, every time they opened the walk. Nothing else in the package reaches it: every
// other guided session here holds at least one set unrecorded.

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/entity"
)

// TestAGuidedWalkWithEverySetRecordedOffersTheFinishAndNotItsFirstSet asks the session
// address of a walk whose last set is already recorded, and requires a 303 to that walk's
// finish page. Sent to a pair instead, the person at the end of their walk is offered a set
// they recorded.
func TestAGuidedWalkWithEverySetRecordedOffersTheFinishAndNotItsFirstSet(t *testing.T) {
	caller := uuid.MustParse("77777777-7777-4777-8777-777777777777")
	session := onePairRecorded(false)
	for _, step := range session.Steps {
		for _, set := range step.Sets {
			if step.ID == session.Steps[0].ID && set == session.Progress.Completed[0].SetID {
				continue
			}
			session.Progress.Completed = append(session.Progress.Completed, entity.GuidedCompletion{
				SessionID: session.ID, StepID: step.ID, SetID: set,
				Revision: int64(len(session.Progress.Completed) + 2), CompletedAt: time.Unix(1777000000, 0).UTC()})
		}
	}
	if _, _, ok := session.Pair(len(session.Progress.Completed)); ok {
		t.Fatalf("every one of the %d sets is recorded yet the session still names a pair, so this case "+
			"no longer asks the end of the walk", len(session.Progress.Completed))
	}

	at := "/app/note/notes/" + session.ID.String()
	code, sent, body := guidedWalkSend(t, scopeServer(t, session), at, caller)
	if code != http.StatusSeeOther || sent != at+"/finish" {
		t.Errorf("the session address of a walk with all %d sets recorded answers %d to %q, want a 303 to %s — no "+
			"pair is left to resume, so the person owes its finish: %s",
			len(session.Progress.Completed), code, sent, at+"/finish", body)
	}
}
