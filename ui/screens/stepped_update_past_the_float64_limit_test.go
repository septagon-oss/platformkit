package screens_test

// What a stepped save does with a write count too big for a float64.
//
// The revision a stepped form carries in its continuation state is the condition its
// final action writes against, so the digits that reach the page decide whether the
// save is accepted. A stepped read decodes numbers exactly; a read that decoded
// through float64 would hand the continuation 9007199254740992 for a row at
// 9007199254740993, and the person standing at the last step would be refused against
// a revision the row was never at, with their answers lost. The page that quotes the
// count to a restore is pinned elsewhere; this pins the count the form writes with.

import (
	"testing"
)

func TestASteppedUpdateWritesARowWhoseRevisionAFloat64CannotHold(t *testing.T) {
	h, admin := countedNoteServer(t)
	item := savedSteppedNote(t, h)
	const large = 9007199254740993
	if _, err := admin.ExecContext(t.Context(), `UPDATE notes SET revision = $1`, large); err != nil {
		t.Fatal(err)
	}
	answers := readyNote(t, h, item+"/edit", "Saved past the limit")
	if out := postStep(h, item, answers, "submit", nil); out.Code != 303 {
		t.Fatalf("the last step of a row at revision %d = %d, want the save accepted: %s", large, out.Code, out.Body)
	}
	if got := savedRowRevision(t, admin); got != large+1 {
		t.Fatalf("the save left the row at revision %d, want %d: a save that quotes the rounded count writes the wrong row", got, large+1)
	}
}
