package screens_test

// A refusal the person cannot answer is a dead end, not a concurrency guard.
//
// A stepped save that quotes a write count the row has passed is refused, and the
// page that says no is useful only because of the count it hands back: the person
// answers by sending that count, and if the number in the refusal is not the one
// the write door takes, their answer meets the same refusal again. Past 2^53 that
// is where the two can part — 9007199254740992 is the largest integer a float64
// holds exactly, so the count this row moves to is the first with no representation
// of its own. The case walks a row to its last step, has the row moved behind the
// continuation, is refused, and asks both halves of what the person can do next:
// the count the refused page still carries is refused a second time, and the count
// that stood in its notice is taken — the save goes through, and the answer the
// person came in with is the one the row ends on.

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestARefusedSteppedSaveIsAnsweredWithTheCountItsRefusalNames(t *testing.T) {
	h, admin := countedNoteServer(t)
	item := savedSteppedNote(t, h)
	const read = 9007199254740992
	if _, err := admin.ExecContext(t.Context(), `UPDATE notes SET revision = $1`, read); err != nil {
		t.Fatal(err)
	}
	answers := readyNote(t, h, item+"/edit", "Answered after the refusal")
	if _, err := admin.ExecContext(t.Context(),
		`UPDATE notes SET revision = $1, title = 'Saved by somebody else'`, read+1); err != nil {
		t.Fatal(err)
	}
	refused := postStep(h, item, answers, "submit", nil)
	if refused.Code != http.StatusPreconditionFailed {
		t.Fatalf("saving over a row that moved from %d to %d = %d, want the refusal the person can answer: %s",
			read, read+1, refused.Code, refused.Body)
	}
	held := continuation(t, refused, http.StatusPreconditionFailed, 2)
	stale := held // the state the refused page hands the browser back, before this case corrects it
	handing := strconv.FormatInt(read+1, 10)
	if row := strconv.Itoa(savedRowRevision(t, admin)); row != handing {
		t.Fatalf("the row counts itself %s, want %s: the fixture's own move did not happen", row, handing)
	}
	if notice := refusalNotice(refused); !strings.Contains(notice, handing) {
		t.Fatalf("the refusal notice holds %q while the row counts itself %s, so the count it hands the person is not the one a save can send back: %s",
			notice, handing, refused.Body)
	}
	if again := postStep(h, item, stale, "submit", nil); again.Code != http.StatusPreconditionFailed {
		t.Fatalf("answering the refusal with the count the page still carries (%s) = %d, want the same refusal again: %s",
			stale.Revision, again.Code, again.Body)
	}
	held.Revision = handing
	if saved := postStep(h, item, held, "submit", nil); saved.Code != http.StatusSeeOther {
		t.Fatalf("answering the refusal with revision %s = %d, want the save accepted: %s",
			handing, saved.Code, saved.Body)
	}
	var title string
	var revision int
	if err := admin.QueryRowContext(t.Context(), `SELECT title, revision FROM notes`).Scan(&title, &revision); err != nil {
		t.Fatal(err)
	}
	if title != "Answered after the refusal" || int64(revision) != read+2 {
		t.Fatalf("the row the person ends on holds title %q at revision %d, want their answer at %d",
			title, revision, read+2)
	}
}

// refusalNotice answers the text of the live region the page carried its refusal in:
// what the person reads when the save is refused, and nothing of the form around it.
func refusalNotice(response *httptest.ResponseRecorder) string {
	body := response.Body.String()
	at := strings.Index(body, `role="alert"`)
	if at < 0 {
		return ""
	}
	if end := strings.Index(body[at:], "</div>"); end >= 0 {
		return body[at : at+end]
	}
	return body[at:]
}
