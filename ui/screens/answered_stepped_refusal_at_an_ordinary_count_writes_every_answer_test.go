package screens_test

// A stepped save refused over an ordinary write count, answered, owes the row every answer.
//
// The person who loses the race over a stepped note has one move: send back the count
// the refusal names, with the answers the continuation still carries. The write that
// move starts must report success and keep every step's answer, with one event beside
// it — and it must do so at the second write of an ordinary row, not only at the counts
// past 2^53 where the digits can round. This leaves one note, holds its last step open
// while another writer saves, answers the refusal with the count the row counts itself
// at, then asks the row whose title, body and rank it holds and how many events it emitted.

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestARefusedSteppedSaveAnsweredAtAnOrdinaryCountWritesEveryAnswer(t *testing.T) {
	h, admin := countedNoteServer(t)
	item := savedSteppedNote(t, h)
	answers := readyNote(t, h, item+"/edit", "Mine, kept across the refusal")
	answers.Answers["body"] = "Written on the second step"
	answers.Answers["rank"] = "9"
	var eventsBefore int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox`).Scan(&eventsBefore); err != nil {
		t.Fatal(err)
	}
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
	hand := strconv.Itoa(savedRowRevision(t, admin))
	if notice := refusalNotice(refused); !strings.Contains(notice, hand) {
		t.Fatalf("the refusal notice holds %q while the row counts itself %s, so no count on that page answers it: %s",
			notice, hand, refused.Body)
	}
	held.Revision = hand
	if saved := postStep(h, item, held, "submit", nil); saved.Code != http.StatusSeeOther {
		t.Fatalf("answering the refusal with revision %s = %d, want the save accepted: %s", hand, saved.Code, saved.Body)
	}
	var title, body string
	var rank int
	if err := admin.QueryRowContext(t.Context(), `SELECT title, body, rank FROM notes`).Scan(&title, &body, &rank); err != nil {
		t.Fatal(err)
	}
	if title != "Mine, kept across the refusal" || body != "Written on the second step" || rank != 9 {
		t.Fatalf("the answered refusal left title %q, body %q, rank %d, want every answer the person held: their own title, %q and 9",
			title, body, rank, "Written on the second step")
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
