package screens_test

// The smallest pair of write counts a float64 collides, refused by a stepped save.
//
// A stepped form posts the write count it read as the condition of its final action,
// so every digit of that count has to survive the read, the continuation and the
// comparison. 9007199254740992 is the largest integer a float64 holds exactly, and
// 9007199254740993 is the first integer that has no representation of its own: the
// two are the same float64, and this pair is where a comparison that went through
// float64 starts accepting a stale save — one count of drift is enough. The row is
// put at the first count, its stepped edit is walked with that count, the row is
// moved to the second behind the continuation, and the save must be refused: nothing
// written, nothing emitted, the newer save left where it is, and the person's answer
// and count still held on the page that says no.

import (
	"net/http"
	"strconv"
	"testing"
)

func TestASteppedSaveRefusesTheFirstCountAFloat64Collides(t *testing.T) {
	h, admin := countedNoteServer(t)
	item := savedSteppedNote(t, h)
	const read = 9007199254740992
	if _, err := admin.ExecContext(t.Context(), `UPDATE notes SET revision = $1`, read); err != nil {
		t.Fatal(err)
	}
	answers := readyNote(t, h, item+"/edit", "Walked to the boundary")
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
		t.Fatalf("saving over a row that moved from %d to %d = %d, want the refusal that names the count it read",
			read, read+1, out.Code)
	}
	var title string
	var outboxAfter int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT title, (SELECT count(*) FROM platformkit_outbox) FROM notes`).Scan(&title, &outboxAfter); err != nil {
		t.Fatal(err)
	}
	if title != "Saved by somebody else" || outboxAfter != outboxBefore {
		t.Fatalf("the refusal wrote title %q and %d outbox rows beside the %d it started with: a refusal commits and emits nothing",
			title, outboxAfter-outboxBefore, outboxBefore)
	}
}
