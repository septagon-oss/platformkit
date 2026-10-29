package rest_test

// A tag the row has passed stays a refusal at counts a float64 cannot separate.
//
// 9007199254740992 and 9007199254740993 are two counts and one float64. Compared
// through one, the caller holding the older tag overwrites a row that had moved: no
// 412, nothing said, the newer write gone. So the quoted count is read and compared
// as an exact integer, the refusal names the count the row is on — the number the
// caller's next write quotes — and the refusal writes and emits nothing.

import (
	"net/http"
	"strings"
	"testing"
)

func TestAStaleTagAFloat64CannotTellFromTheRowsCountIsRefused(t *testing.T) {
	w := memos(t)
	const read = 9007199254740992 // 2^53, the count a caller holds; the row reaches 2^53+1 below
	if _, err := w.admin.ExecContext(t.Context(),
		`UPDATE rest_memos SET revision = $1 WHERE id = $2`, read, w.memo); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := quoted(t, w.router, http.MethodPatch, w.address(), `{"title":"The write that moved it"}`, ""); code != http.StatusOK {
		t.Fatalf("PATCH with no tag = %d, want the write that moves the row past the count the caller holds", code)
	}

	code, body, _ := quoted(t, w.router, http.MethodPatch, w.address(), `{"title":"Written from a passed count"}`, `"9007199254740992"`)
	if code != http.StatusPreconditionFailed {
		t.Fatalf("PATCH quoting the count the row was on = %d %s, want 412: a float64 sees one count where the row holds two", code, body)
	}
	if !strings.Contains(body, "9007199254740993") {
		t.Fatalf("the refusal %s names no count the caller could quote back, want the row's own 9007199254740993", body)
	}
	if title, _, revision := w.readBack(t); revision != read+1 || title != "The write that moved it" {
		t.Fatalf("the refused write left the row as %q at revision %d, want %q at %d: a refusal writes nothing",
			title, revision, "The write that moved it", read+1)
	}
	if n := len(w.kept.kept); n != 2 {
		t.Fatalf("the refused write left %d kept versions, want 2, the create and the one accepted write: a refusal emits nothing", n)
	}
}
