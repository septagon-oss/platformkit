package screens_test

// Two addresses of a guided walk belong to opposite halves of it. The summary is what a finish
// returns, so a session still under a person's hands has no summary to show and asking for one
// returns them to the session address; and once a finish has landed, every page of that walk
// still standing — the session address, a step, the rest after a set, the finish page — sends
// the person to that summary rather than leaving them on a page whose sets are recorded. Both
// answers are a 303 with no body, so the only thing a person would notice if either guard went
// missing is a step page that keeps offering sets already recorded, or the end of a walk that
// never ended. Nothing asked either address across that line: the guided cases ask each page
// what it renders, and the cases that ask for a summary ask after the finish, where the answer
// is a page. This one asks both halves, and reaches each answer through the address the walk is
// sent to, which is what a browser sees when the guard holds.

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

// guidedWalkSend asks one address of one walk the way a browser at the workspace does and hands
// back the status and the address it was sent to, alongside the start of the body for the trace.
func guidedWalkSend(t *testing.T, h http.Handler, address string, caller uuid.UUID) (int, string, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "http://"+formHost+address, nil)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("X-Test-Caller", caller.String())
	req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	return res.Code, res.Header().Get("Location"), firstWords(res.Body.String())
}

// TestAGuidedWalkSendsAPersonWhoFinishedToItsSummaryAndOneWhoHasNotAwayFromIt answers a finished
// session's own address, one of its steps, the rest after that step's set and its finish page,
// and asks each to be a 303 to that session's summary; then asks the summary of a session still
// under way to be a 303 back to the session address it resumes. A walk that kept a finished
// person on a step page leaves them recording sets they recorded; a walk that answered a summary
// before any finish returned one shows an end nobody declared.
func TestAGuidedWalkSendsAPersonWhoFinishedToItsSummaryAndOneWhoHasNotAwayFromIt(t *testing.T) {
	caller := uuid.MustParse("66666666-6666-4666-8666-666666666666")
	ended := onePairRecorded(true)
	at := "/app/note/notes/" + ended.ID.String()
	h := scopeServer(t, ended)
	step := ended.Steps[0]
	for _, address := range []string{
		at,
		at + "/steps/" + step.ID.String() + "/sets/" + step.Sets[0].String(),
		at + "/rest/" + step.ID.String() + "/" + step.Sets[0].String(),
		at + "/finish",
	} {
		code, sent, body := guidedWalkSend(t, h, address, caller)
		if code != http.StatusSeeOther || sent != at+"/summary" {
			t.Errorf("%s of a walk that ended answers %d to %q, want a 303 to %s — the sets on that page are "+
				"recorded, so the person owes its summary and nothing else: %s", address, code, sent, at+"/summary", body)
		}
	}

	underWay := onePairRecorded(false)
	at = "/app/note/notes/" + underWay.ID.String()
	code, sent, body := guidedWalkSend(t, scopeServer(t, underWay), at+"/summary", caller)
	if code != http.StatusSeeOther || sent != at {
		t.Errorf("the summary of a walk still under %s answers %d to %q, want a 303 back to %s — no finish returned "+
			"that summary, so there is no end to show yet: %s", caller, code, sent, at, body)
	}
}
