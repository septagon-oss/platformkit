package rest_test

// An accepted write answers with the row's new count, and that answer is the tag the
// next write quotes back. Answered through a float64 anywhere along the way, it is a
// count the row never was, and the caller's next write is refused 412 against a number
// it did not send. A row parked one past the exact range cannot notice that rounding:
// above 2^53 the gap between representable counts is 2, so the count such a row answers
// with is representable by luck. The first count a rounded answer loses is one above
// the last count a float64 holds exactly — so the row starts there, and the count the
// write hands back is the first one with no representation of its own. Neither write
// here reads the row: the first goes out unquoted, which the door allows, and the second
// is quoted with what the first answered.

import (
	"net/http"
	"testing"
)

func TestAWriteAtTheLastExactCountAnswersTheFirstCountNoFloat64Holds(t *testing.T) {
	w := memos(t)
	const at = 9007199254740992 // 2^53: the last count a float64 holds exactly
	if _, err := w.admin.ExecContext(t.Context(),
		`UPDATE rest_memos SET revision = $1 WHERE id = $2`, at, w.memo); err != nil {
		t.Fatal(err)
	}

	code, body, head := quoted(t, w.router, http.MethodPatch, w.address(), `{"title":"First edit"}`, "")
	if code != http.StatusOK {
		t.Fatalf("PATCH with no tag = %d %s, want the write the door allows a caller that read no tag", code, body)
	}
	answered := head.Get("ETag")
	if answered != `"9007199254740993"` {
		t.Fatalf("the write of a row at %d answers ETag %s, want \"9007199254740993\": the first count no float64 holds is the one the next write has to quote", at, answered)
	}

	code, body, _ = quoted(t, w.router, http.MethodPatch, w.address(), `{"title":"Second edit"}`, answered)
	if code != http.StatusOK {
		t.Fatalf("PATCH quoting the tag the write answered (%s) = %d %s, want the second write of a row nobody else touched", answered, code, body)
	}
	if title, _, revision := w.readBack(t); revision != at+2 || title != "Second edit" {
		t.Fatalf("the chained writes left the row as %q at revision %d, want \"Second edit\" at %d", title, revision, at+2)
	}
}
