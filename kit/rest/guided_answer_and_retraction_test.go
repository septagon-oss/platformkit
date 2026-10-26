package rest_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// A guided command answers with the receipt the session retains, because the
// kernel compares the two inside the command's own transaction: a module that
// records one thing and returns another gets no answer, no row and no event.
func TestAGuidedCommandAnswersNoReceiptTheSessionDoesNotHold(t *testing.T) {
	h := newGuided(t, func(spec *rest.SessionSpec) {
		recorded := spec.Session.Complete.Run
		spec.Session.Complete.Run = func(ctx context.Context, tx db.Tx[db.Tenant], actor, id uuid.UUID, in entity.CompleteGuidedSet) (entity.GuidedCompletion, error) {
			out, err := recorded(ctx, tx, actor, id, in)
			if err != nil {
				return out, err
			}
			out.CompletedAt = guidedAt.Add(time.Hour)
			return out, nil
		}
	})
	in := guidedInput(0, 1)
	body, _ := json.Marshal(in)
	res := guidedCall(h.router, "POST", "/api/v1/practice/sessions/"+guidedID.String()+"/complete-set", string(body), "application/json", "a-mismatched-answer")
	if res.Code < 400 {
		t.Fatalf("a receipt the session does not hold was answered: %d %s", res.Code, res.Body.String())
	}
	if state := h.read(t); state.Progress.Revision != 1 || len(state.Progress.Completed) != 0 {
		t.Fatalf("the refused command left progress behind: %+v", state.Progress)
	}
	if got := h.events(t); got != 0 {
		t.Fatalf("the refused command emitted %d event(s)", got)
	}
}

// The step screen reads the session through the resource's own read, so a read
// the module withdraws takes the page with it: no step, no copy, no progress.
func TestAGuidedScreenRefusesAReadTheModuleRetracts(t *testing.T) {
	h := newGuided(t)
	mountGuidedScreens(h)
	web := "/app/practice/sessions/" + guidedID.String()
	step := guidedInput(0, 1)
	address := fmt.Sprintf("%s/steps/%s/sets/%s", web, step.StepID, step.SetID)
	get := guidedCall(h.router, "GET", address, "", "", "")
	if get.Code != 200 || !strings.Contains(get.Body.String(), "Place the two strips beside the frame.") {
		t.Fatalf("the step a person is on: %d", get.Code)
	}
	if _, err := h.admin.ExecContext(t.Context(), "UPDATE guided_runs SET active=false"); err != nil {
		t.Fatal(err)
	}
	get = guidedCall(h.router, "GET", address, "", "", "")
	if body := get.Body.String(); get.Code == 200 || strings.Contains(body, "Practice sequence") || strings.Contains(body, "Strips") {
		t.Fatalf("a withdrawn read still renders the session: %d %s", get.Code, body)
	}
}

// Without JavaScript the form is the only way forward, so the same form sent
// twice records the set once: the second answer is the first receipt, and the
// second send emits no event and moves no revision.
func TestAGuidedNativeFormRecordsASetOnce(t *testing.T) {
	h := newGuided(t)
	mountGuidedScreens(h)
	web := "/app/practice/sessions/" + guidedID.String()
	in := guidedInput(0, 1)
	body := url.Values{"stepId": {in.StepID.String()}, "setId": {in.SetID.String()}, "ifRevision": {"1"}}.Encode()
	var first string
	for range 2 {
		res := guidedCall(h.router, "POST", web+"/complete-set", body, "application/x-www-form-urlencoded", "")
		if res.Code != 303 {
			t.Fatalf("native completion: %d %s", res.Code, res.Body.String())
		}
		if first == "" {
			first = res.Header().Get("Location")
		} else if res.Header().Get("Location") != first {
			t.Fatalf("the second send walked somewhere new: %s then %s", first, res.Header().Get("Location"))
		}
	}
	if got := h.events(t); got != 1 {
		t.Fatalf("a replayed form emitted %d event(s), want 1", got)
	}
	if state := h.read(t); state.Progress.Revision != 2 || len(state.Progress.Completed) != 1 {
		t.Fatalf("a replayed form moved the session twice: %+v", state.Progress)
	}
}

// A person's phone builds its screens from the catalogue, so a guided session is
// catalogued as one resource with exactly the two commands its module declared.
func TestAGuidedSessionIsCataloguedWithBothCommands(t *testing.T) {
	h := newGuided(t)
	var found *int
	resources := h.api.Resources()
	for i, r := range resources {
		if r.Module == "practice" && r.Entity == "session" {
			found = &i
		}
	}
	if found == nil {
		t.Fatal("no guided session in the catalogue")
	}
	verbs := map[string]bool{}
	for _, c := range resources[*found].Commands {
		verbs[c.Verb] = true
	}
	if !verbs["complete-set"] || !verbs["finish"] {
		t.Fatalf("the catalogue names %v, and a phone can only offer what it is told", verbs)
	}
}
