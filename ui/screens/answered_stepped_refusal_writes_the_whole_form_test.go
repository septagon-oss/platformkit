package screens_test

// The answer to a refused stepped save owes the row the whole form and one event.
//
// A stepped save refused over a write count the row has passed leaves the person one
// move: send back the count the refusal names, with the answers the continuation
// still carries. What that write owes is every answer of the form — the title that
// named the step, the body and rank of the step before it — and one outbox event
// beside it, since domain state and its event commit together. A retry that reached
// the row with only what the refusal showed, or with no event at all, would still
// answer 303 and leave the row shorter than the form the person filled in. This
// walks a row at 9007199254740992 — the largest integer a float64 holds exactly, so
// the count one past it is the first the transport can round anywhere — to its last
// step, has the row moved behind the continuation, is refused, and answers with the
// count the refusal named, then asks the row what it holds and how many events it
// emitted.

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestTheAnswerOfARefusedSteppedSaveWritesEveryAnswerAndOneEvent(t *testing.T) {
	h, admin := countedNoteServer(t)
	item := savedSteppedNote(t, h)
	const read = 9007199254740992
	if _, err := admin.ExecContext(t.Context(), `UPDATE notes SET revision = $1`, read); err != nil {
		t.Fatal(err)
	}
	answers := readyNote(t, h, item+"/edit", "Answered after the refusal")
	answers.Answers["body"] = "Written after the refusal"
	answers.Answers["rank"] = "9"
	if _, err := admin.ExecContext(t.Context(),
		`UPDATE notes SET revision = $1, title = 'Saved by somebody else'`, read+1); err != nil {
		t.Fatal(err)
	}
	var eventsBefore int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox`).Scan(&eventsBefore); err != nil {
		t.Fatal(err)
	}
	refused := postStep(h, item, answers, "submit", nil)
	if refused.Code != http.StatusPreconditionFailed {
		t.Fatalf("saving over a row that moved from %d to %d = %d, want the refusal the person can answer: %s",
			read, read+1, refused.Code, refused.Body)
	}
	held := continuation(t, refused, http.StatusPreconditionFailed, 2)
	hand := strconv.Itoa(savedRowRevision(t, admin))
	if notice := refusalNotice(refused); !strings.Contains(notice, hand) {
		t.Fatalf("the refusal notice holds %q while the row counts itself %s, so no count on that page answers it: %s",
			notice, hand, refused.Body)
	}
	held.Revision = hand
	if saved := postStep(h, item, held, "submit", nil); saved.Code != http.StatusSeeOther {
		t.Fatalf("answering the refusal with revision %s = %d, want the save accepted: %s",
			hand, saved.Code, saved.Body)
	}
	var title, body string
	var rank int
	if err := admin.QueryRowContext(t.Context(), `SELECT title, body, rank FROM notes`).Scan(&title, &body, &rank); err != nil {
		t.Fatal(err)
	}
	if title != "Answered after the refusal" || body != "Written after the refusal" || rank != 9 {
		t.Fatalf("the answered refusal left title %q, body %q, rank %d, want every answer the person held: their title, %q and 9",
			title, body, rank, "Written after the refusal")
	}
	var eventsAfter int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox`).Scan(&eventsAfter); err != nil {
		t.Fatal(err)
	}
	if eventsAfter != eventsBefore+1 {
		t.Fatalf("the answered refusal emitted %d outbox rows beside the %d it started with, want the one event its commit came with",
			eventsAfter, eventsBefore)
	}
}
