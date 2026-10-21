package internal_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/decide/contracts"
	"github.com/septagon-oss/platformkit/modules/decide/contracts/decidetest"
)

// Creator-only commands are the module's own duty, enforced from its rows with
// no policy server composed: a participant who holds decide:manage still cannot
// close somebody else's question, a non-member who holds decide:read cannot read
// its tally, and one person's rewind credential acts for nobody else.
func TestOnlyTheCreatorClosesAndOnlyTheVoterRewindsTheirOwnBallot(t *testing.T) {
	w := newWorld(t)
	w.run(t, func(ctx context.Context, tx db.Tx[db.Tenant]) error { return put(ctx, tx, decidetest.Seed()) })
	_, handler := mountedDecide(t, w)
	as := func(person uuid.UUID) { w.ctx = tenancy.WithPrincipal(w.ctx, tenancy.Principal{UserID: person}) }

	as(decidetest.Bob)
	closed := request(t, handler, http.MethodPost, "/api/v1/decide/questions/"+decidetest.Q.String()+"/close", `{"ifRevision":1}`)
	if closed.Code != http.StatusForbidden || strings.Contains(closed.Body.String(), `"revision"`) {
		t.Fatalf("a manager who is not the creator closed the question: %d %s", closed.Code, closed.Body.String())
	}
	var revision int
	var status, emitted string
	if err := w.admin.QueryRowContext(t.Context(), `SELECT revision, status, (SELECT coalesce(string_agg(name, ','), '') FROM platformkit_outbox WHERE name LIKE 'decide.%') FROM decide_questions WHERE id=$1`, decidetest.Q).Scan(&revision, &status, &emitted); err != nil {
		t.Fatal(err)
	}
	if revision != 1 || status != contracts.StatusOpen || emitted != "" {
		t.Fatalf("refused close wrote something: revision=%d status=%s decide events=%q", revision, status, emitted)
	}

	as(decidetest.Evan)
	tally := request(t, handler, http.MethodGet, "/api/v1/decide/questions/"+decidetest.Q.String()+"/results", "")
	if tally.Code != http.StatusForbidden || strings.Contains(tally.Body.String(), `"voters"`) || strings.Contains(tally.Body.String(), `"counts"`) {
		t.Fatalf("a non-member read the tally: %d %s", tally.Code, tally.Body.String())
	}

	as(decidetest.Alice)
	cast := request(t, handler, http.MethodPost, "/api/v1/decide/cards/"+decidetest.B.String()+"/yes", `{"questionId":"`+decidetest.Q.String()+`","ifRevision":0}`)
	var reply struct {
		UndoToken string `json:"undoToken"`
	}
	if err := json.Unmarshal(cast.Body.Bytes(), &reply); err != nil || cast.Code != http.StatusOK || reply.UndoToken == "" {
		t.Fatalf("cast: %d %s %v", cast.Code, cast.Body.String(), err)
	}
	as(decidetest.Bob)
	undo := request(t, handler, http.MethodPost, "/api/v1/decide/cards/undo-vote", `{"token":"`+reply.UndoToken+`"}`)
	if undo.Code != http.StatusForbidden {
		t.Fatalf("another person used the voter's rewind credential: %d %s", undo.Code, undo.Body.String())
	}
	var value string
	var ballotRevision int
	if err := w.admin.QueryRowContext(t.Context(), `SELECT value, revision FROM decide_votes WHERE option_id=$1 AND person_id=$2`, decidetest.B, decidetest.Alice).Scan(&value, &ballotRevision); err != nil {
		t.Fatal(err)
	}
	if value != "yes" || ballotRevision != 1 {
		t.Fatalf("the refused rewind changed the ballot: %s rev=%d", value, ballotRevision)
	}
}
