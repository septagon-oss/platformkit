package rest_test

// A delete whose tag the row has passed stays a refusal at counts a float64 cannot
// separate, and the row it refused to remove is still there.
//
// 9007199254740992 and 9007199254740993 are two counts and one float64. Compared
// through one, a caller that read the older count deletes a row that had moved since:
// a lost row rather than a lost write, said nowhere. So the quoted count is compared
// as an exact integer, the refusal names the count the row is on — the number the
// caller's next delete quotes — and the refusal removes and emits nothing.

import (
	"net/http"
	"strings"
	"testing"
)

func TestAStaleTagPastTheFloat64LimitRefusesADeleteAndLeavesTheRow(t *testing.T) {
	w := memos(t)
	const read = 9007199254740992 // 2^53, the count a caller holds; the row reaches 2^53+1 below
	if _, err := w.admin.ExecContext(t.Context(),
		`UPDATE rest_memos SET revision = $1 WHERE id = $2`, read, w.memo); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := quoted(t, w.router, http.MethodPatch, w.address(), `{"title":"The write that moved it"}`, ""); code != http.StatusOK {
		t.Fatalf("PATCH with no tag = %d, want the write that moves the row past the count the caller holds", code)
	}

	code, body, _ := quoted(t, w.router, http.MethodDelete, w.address(), "", `"9007199254740992"`)
	if code != http.StatusPreconditionFailed {
		t.Fatalf("DELETE quoting the count the row was on = %d %s, want 412: a float64 sees one count where the row holds two", code, body)
	}
	if !strings.Contains(body, "9007199254740993") {
		t.Fatalf("the refusal %s names no count the caller could quote back, want the row's own 9007199254740993", body)
	}
	if title, _, revision := w.readBack(t); revision != read+1 || title != "The write that moved it" {
		t.Fatalf("the refused delete left the row as %q at revision %d, want %q at %d: a refusal removes nothing",
			title, revision, "The write that moved it", read+1)
	}
}
