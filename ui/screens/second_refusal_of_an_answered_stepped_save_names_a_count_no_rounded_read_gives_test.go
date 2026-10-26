package screens_test

// A refusal after the person's own save names a count no rounded read could print.
//
// A stepped save refused over a write count the row has passed is answered with the
// count the row counts itself at, and that answer is a page's worth of state, so the
// same continuation can arrive a second time — a back button over a saved form, a
// submit pressed twice. That second post is refused, and its refusal has to name a
// count a save can take, with every digit of it the row's: at 2^53+3 a read that
// rounded anywhere hands back ...96, and a refusal naming that count is refused again
// by the very write it asks for. This takes the row past the boundary, answers the
// first refusal, posts the answer a second time, and asks what the second refusal
// hands back and what the save answered with it leaves behind.

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestTheSecondRefusalOfAnAnsweredSteppedSaveNamesACountNoRoundedReadGives(t *testing.T) {
	h, admin := countedNoteServer(t)
	item := savedSteppedNote(t, h)
	const quoted = 9007199254740994 // 2^53+2: the last write count of this pair a float64 holds exactly
	const moved = quoted + 1        // 2^53+3: a read that rounded anywhere prints ...96 for this
	const rounded = "9007199254740996"

	answers := readyNote(t, h, item+"/edit", "Mine, held across the boundary")
	if _, err := admin.ExecContext(t.Context(),
		`UPDATE notes SET revision = $1, title = 'Saved by somebody else'`, quoted); err != nil {
		t.Fatal(err)
	}
	refused := postStep(h, item, answers, "submit", nil)
	if refused.Code != http.StatusPreconditionFailed {
		t.Fatalf("saving over the row that moved to %d = %d, want the refusal the person can answer: %s",
			quoted, refused.Code, refused.Body)
	}
	held := continuation(t, refused, http.StatusPreconditionFailed, 2)
	held.Revision = strconv.Itoa(savedRowRevision(t, admin))
	if held.Revision != strconv.FormatInt(quoted, 10) {
		t.Fatalf("the row counts itself %s after that write, want %d: the case's boundary moved", held.Revision, quoted)
	}
	if saved := postStep(h, item, held, "submit", nil); saved.Code != http.StatusSeeOther {
		t.Fatalf("answering the first refusal with %s = %d, want the save accepted: %s",
			held.Revision, saved.Code, saved.Body)
	}
	var eventsOnce int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox`).Scan(&eventsOnce); err != nil {
		t.Fatal(err)
	}
	second := postStep(h, item, held, "submit", nil)
	if second.Code != http.StatusPreconditionFailed {
		t.Fatalf("posting the answered refusal a second time = %d, want the refusal of a write over the row at %d: %s",
			second.Code, moved, second.Body)
	}
	hands := strconv.Itoa(savedRowRevision(t, admin))
	notice := refusalNotice(second)
	if hands != strconv.FormatInt(moved, 10) {
		t.Fatalf("the row counts itself %s, want %d: the accepted write moved it somewhere else", hands, moved)
	}
	if !strings.Contains(notice, hands) || strings.Contains(notice, rounded) {
		t.Fatalf("the second refusal holds %q while the row counts itself %s: it must name that count exactly, never the rounded %s, or the person is handed a count the next write refuses",
			notice, hands, rounded)
	}
	retry := continuation(t, second, http.StatusPreconditionFailed, 2)
	retry.Revision = hands
	retry.Answers["title"] = "Mine, answered past the boundary"
	if saved := postStep(h, item, retry, "submit", nil); saved.Code != http.StatusSeeOther {
		t.Fatalf("answering the second refusal with the count the row is on (%s) = %d, want the person's own save accepted: %s",
			hands, saved.Code, saved.Body)
	}
	var title string
	var revision, eventsTwice int
	if err := admin.QueryRowContext(t.Context(), `SELECT title, revision FROM notes`).Scan(&title, &revision); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox`).Scan(&eventsTwice); err != nil {
		t.Fatal(err)
	}
	if title != "Mine, answered past the boundary" || revision != moved+1 || eventsTwice != eventsOnce+1 {
		t.Fatalf("the answer to the second refusal left title %q at revision %d with %d events, want %q at revision %d with %d events",
			title, revision, eventsTwice, "Mine, answered past the boundary", moved+1, eventsOnce+1)
	}
}
