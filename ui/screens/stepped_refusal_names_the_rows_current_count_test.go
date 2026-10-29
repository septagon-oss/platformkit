package screens_test

// A refusal that says "the row moved" has to say it with the row's own count.
//
// A stepped save posts the write count it read as the condition of its final action,
// and the 412 it is refused with is worth anything only because it names what the row
// counts itself as now: told a count the row is not on, the person cannot answer,
// because the count they are handed is refused again. Past 2^53 that is where the
// sentence can break — 9007199254740992 is the largest integer a float64 holds
// exactly, so the count the row moved to is the first one a read of the row that
// rounded anywhere gives back as the count the person already quoted. The refusal has
// to carry it exactly, and still hold the answer and count the person came in with.

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestASteppedRefusalNamesTheCountTheRowIsActuallyOn(t *testing.T) {
	h, admin := countedNoteServer(t)
	item := savedSteppedNote(t, h)
	const read = 9007199254740992
	if _, err := admin.ExecContext(t.Context(), `UPDATE notes SET revision = $1`, read); err != nil {
		t.Fatal(err)
	}
	answers := readyNote(t, h, item+"/edit", "Held across the boundary")
	if _, err := admin.ExecContext(t.Context(),
		`UPDATE notes SET revision = $1, title = 'Saved by somebody else'`, read+1); err != nil {
		t.Fatal(err)
	}
	out := postStep(h, item, answers, "submit", nil)
	if out.Code != http.StatusPreconditionFailed {
		t.Fatalf("saving over a row that moved from %d to %d = %d, want the refusal that names the row: %s",
			read, read+1, out.Code, out.Body)
	}
	if quoted := strconv.FormatInt(read+1, 10); !strings.Contains(out.Body.String(), quoted) {
		t.Fatalf("the refusal never names the count the row is on (%s), so the person cannot send it back: %s",
			quoted, out.Body)
	}
	kept := continuation(t, out, http.StatusPreconditionFailed, 2)
	if kept.Revision != strconv.FormatInt(read, 10) || kept.Answers["title"] != "Held across the boundary" {
		t.Fatalf("the refusal lost what the person had: revision %q, answers %+v", kept.Revision, kept.Answers)
	}
}
