package screens_test

// An answered stepped save spends the answer it was given.
//
// A person whose stepped save was refused has one move: send back the count the
// refusal names with the answers the continuation still carries. The page that
// refusal leaves open still holds that continuation afterwards, so the same answer
// can arrive a second time — a back button pressed over a saved form, a submit
// pressed twice. The row is one write past the count that answer quotes by then, so
// the second post is a mutation over a row that moved: it is refused, and a refused
// mutation writes nothing, emits nothing and leaves the row as the accepted answer
// left it. This refuses a save, answers it once, posts that same answer again, and
// then asks the row whose title it holds, what count it counts itself at, and how
// many events it emitted.

import (
	"net/http"
	"strconv"
	"testing"
)

func TestAnAnsweredSteppedSaveRefusesASecondPostOfTheSameAnswer(t *testing.T) {
	h, admin := countedNoteServer(t)
	item := savedSteppedNote(t, h)
	answers := readyNote(t, h, item+"/edit", "Mine, kept across the refusal")
	if _, err := admin.ExecContext(t.Context(),
		`UPDATE notes SET revision = revision + 1, title = 'Saved by somebody else'`); err != nil {
		t.Fatal(err)
	}
	refused := postStep(h, item, answers, "submit", nil)
	if refused.Code != http.StatusPreconditionFailed {
		t.Fatalf("saving over the row that moved while the last step was open = %d, want the refusal the person can answer: %s",
			refused.Code, refused.Body)
	}
	held := continuation(t, refused, http.StatusPreconditionFailed, 2)
	held.Revision = strconv.Itoa(savedRowRevision(t, admin))
	if saved := postStep(h, item, held, "submit", nil); saved.Code != http.StatusSeeOther {
		t.Fatalf("answering the refusal with revision %s = %d, want the save accepted: %s",
			held.Revision, saved.Code, saved.Body)
	}
	var title string
	var revision, events int
	if err := admin.QueryRowContext(t.Context(), `SELECT title, revision FROM notes`).Scan(&title, &revision); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	replayed := postStep(h, item, held, "submit", nil)
	if replayed.Code != http.StatusPreconditionFailed {
		t.Fatalf("posting the answered refusal a second time = %d, want the refusal of a write over the row at %d: %s",
			replayed.Code, revision, replayed.Body)
	}
	var titleAfter string
	var revisionAfter, eventsAfter int
	if err := admin.QueryRowContext(t.Context(), `SELECT title, revision FROM notes`).Scan(&titleAfter, &revisionAfter); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox`).Scan(&eventsAfter); err != nil {
		t.Fatal(err)
	}
	if titleAfter != "Mine, kept across the refusal" || revisionAfter != revision || eventsAfter != events {
		t.Fatalf("the refused second post left title %q at revision %d with %d events, want %q at revision %d with %d events",
			titleAfter, revisionAfter, eventsAfter, "Mine, kept across the refusal", revision, events)
	}
}
