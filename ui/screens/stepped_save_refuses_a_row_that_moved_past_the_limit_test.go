package screens_test

// Two write counts a float64 cannot tell apart are two counts to a stepped save.
//
// A stepped form holds the write count it read in its continuation state and posts
// that as the condition of its final action. A count beyond 2^53 is where that
// matters: 9007199254740995 and 9007199254740996 are the same float64, so a save
// whose read, transport or comparison rounded anywhere would let the person at the
// last step write over a save that happened while they were filling the form, and
// the row would answer 303 while doing it. This walks one row to its last step,
// moves the row to the next count behind the continuation, and asks the refusal to
// refuse: nothing written, nothing emitted, the row left where the other save left
// it, and the person's own answers and count still held on the page that says no.

import (
	"net/http"
	"strconv"
	"testing"
)

func TestASteppedSaveRefusesARowThatMovedPastTheDigitsItRead(t *testing.T) {
	h, admin := countedNoteServer(t)
	item := savedSteppedNote(t, h)
	const read = 9007199254740995
	if _, err := admin.ExecContext(t.Context(), `UPDATE notes SET revision = $1`, read); err != nil {
		t.Fatal(err)
	}
	answers := readyNote(t, h, item+"/edit", "Half-finished edit")
	if answers.Revision != strconv.FormatInt(read, 10) {
		t.Fatalf("the continuation carries revision %q, want the digits the row was at: %d", answers.Revision, read)
	}
	var outboxBefore int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox`).Scan(&outboxBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(t.Context(),
		`UPDATE notes SET revision = $1, title = 'Saved by somebody else'`, read+1); err != nil {
		t.Fatal(err)
	}
	out := postStep(h, item, answers, "submit", nil)
	if out.Code != http.StatusPreconditionFailed {
		t.Fatalf("saving over a row at revision %d = %d, want the refusal that names the count it read", read+1, out.Code)
	}
	if got := savedRowRevision(t, admin); got != read+1 {
		t.Fatalf("the refused save left the row at revision %d, want %d: the person's stale write replaced the newer one", got, read+1)
	}
	var title string
	var outboxAfter int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT title, (SELECT count(*) FROM platformkit_outbox) FROM notes`).Scan(&title, &outboxAfter); err != nil {
		t.Fatal(err)
	}
	if title != "Saved by somebody else" || outboxAfter != outboxBefore {
		t.Fatalf("the refusal wrote title %q and %d outbox rows beside the %d it started with: a refusal commits and emits nothing", title, outboxAfter-outboxBefore, outboxBefore)
	}
	kept := continuation(t, out, http.StatusPreconditionFailed, 2)
	if kept.Revision != strconv.FormatInt(read, 10) || kept.Answers["title"] != "Half-finished edit" {
		t.Fatalf("the refusal lost what the person had: revision %q, answers %+v", kept.Revision, kept.Answers)
	}
}
