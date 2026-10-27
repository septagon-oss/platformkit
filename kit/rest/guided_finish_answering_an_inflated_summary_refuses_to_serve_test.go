package rest_test

// The finish summary is the page a person leaves with and, under an idempotency
// key, the answer a replay serves again. The mount holds the summary a command
// returns against the one the session kept, so a module that ends a session on a
// total it never recorded answers nothing, writes nothing and emits nothing.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// TestAGuidedFinishAnsweringAnInflatedSummaryRefusesToServeIt posts one partial
// finish twice: once over the fixture's own command, whose summary the session
// keeps, and once over that command with its completed-set count raised to the
// session's total while the revision is left as the session recorded it. The
// second is refused below 400 and leaves no summary, no revision and no event.
func TestAGuidedFinishAnsweringAnInflatedSummaryRefusesToServeIt(t *testing.T) {
	path := "/api/v1/practice/sessions/" + guidedID.String() + "/finish"
	body, err := json.Marshal(entity.FinishGuidedSession{IfRevision: 1})
	if err != nil {
		t.Fatal(err)
	}
	kept := newGuided(t)
	honest := guidedCall(kept.router, "POST", path, string(body), "application/json", "summary-the-session-keeps")
	if honest.Code != 200 || kept.events(t) != 1 {
		t.Fatalf("the finish the session keeps: %d %s (%d events)", honest.Code, honest.Body.String(), kept.events(t))
	}
	inflated := newGuided(t, func(spec *rest.SessionSpec) {
		answers := spec.Session.Finish.Run
		spec.Session.Finish.Run = func(ctx context.Context, tx db.Tx[db.Tenant], actor, id uuid.UUID, in entity.FinishGuidedSession) (entity.GuidedSummary, error) {
			out, err := answers(ctx, tx, actor, id, in)
			if err != nil {
				return out, err
			}
			out.CompletedSets = 4
			return out, nil
		}
	})
	refused := guidedCall(inflated.router, "POST", path, string(body), "application/json", "summary-the-session-never-had")
	if refused.Code < 400 {
		t.Fatalf("a finish answering a summary its session does not keep was served: %d %s", refused.Code, refused.Body.String())
	}
	if strings.Contains(refused.Body.String(), `"completedSets":4`) {
		t.Errorf("the refused finish handed out the summary it invented: %s", refused.Body.String())
	}
	if got := inflated.events(t); got != 0 {
		t.Errorf("a refused finish emitted %d events, want 0", got)
	}
	current := inflated.read(t)
	if current.Progress.Revision != 1 || current.Progress.Finished != nil {
		t.Errorf("a refused finish left the session at revision %d finished=%v, want revision 1 unfinished", current.Progress.Revision, current.Progress.Finished != nil)
	}
}
