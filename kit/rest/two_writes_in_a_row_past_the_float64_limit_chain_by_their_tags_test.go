package rest_test

// A client that writes the same row twice quotes the tag the first write answered
// with, and never reads the row in between.
//
// The read's tag is the well-walked door; the write's own response carries a tag too,
// and that is the one a person who edits a row twice in a row uses. For one row whose
// write count sits at 2^53+1, the first count no float64 holds, the tag the accepted
// write hands back has to hold its digits: rounded, the next write is refused 412
// against a count the row never was, and the caller can only get out by reading the
// row again — a conditional write that cannot be chained is not a condition a client
// can live inside. Neither write here reads the row: the first goes unquoted, which
// the door allows, and the second is quoted with what the first answered.

import (
	"net/http"
	"testing"
)

func TestTwoWritesInARowPastTheFloat64LimitChainByTheirTags(t *testing.T) {
	w := memos(t)
	const at = 9007199254740993 // 2^53+1: the first write count no float64 holds
	if _, err := w.admin.ExecContext(t.Context(),
		`UPDATE rest_memos SET revision = $1 WHERE id = $2`, at, w.memo); err != nil {
		t.Fatal(err)
	}

	code, body, head := quoted(t, w.router, http.MethodPatch, w.address(), `{"title":"First edit"}`, "")
	if code != http.StatusOK {
		t.Fatalf("PATCH with no tag = %d %s, want the write the door allows a caller that read no tag", code, body)
	}
	answered := head.Get("ETag")
	if answered != `"9007199254740994"` {
		t.Fatalf("the write of a row at %d answers ETag %s, want \"9007199254740994\": the count it hands back is the one the next write has to quote", at, answered)
	}

	code, body, _ = quoted(t, w.router, http.MethodPatch, w.address(), `{"title":"Second edit"}`, answered)
	if code != http.StatusOK {
		t.Fatalf("PATCH quoting the tag the write answered (%s) = %d %s, want the second write of a row nobody else touched", answered, code, body)
	}
	if title, _, revision := w.readBack(t); revision != at+2 || title != "Second edit" {
		t.Fatalf("the chained writes left the row as %q at revision %d, want \"Second edit\" at %d", title, revision, at+2)
	}
}
