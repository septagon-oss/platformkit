package rest_test

// A caller quotes the tag it read, and the tag it read is a header.
//
// A refused write names the row's count so the next write can quote it, and what
// the next write quotes is the ETag the read handed out, not a number in a body.
// Both directions have to keep their digits for one row at 2^53+1, the first write
// count no float64 holds: the tag the read answers with, or no client could ever
// name this row to write it, and the tag it sends back, or the write that meant to
// answer the row is refused against a count the row never was. This reads the row
// the way a client reads it and writes with what came back.

import (
	"net/http"
	"testing"
)

func TestTheTagReadFromARowPastTheFloat64LimitWritesIt(t *testing.T) {
	w := memos(t)
	const at = 9007199254740993 // 2^53+1: the first write count no float64 holds
	if _, err := w.admin.ExecContext(t.Context(),
		`UPDATE rest_memos SET revision = $1 WHERE id = $2`, at, w.memo); err != nil {
		t.Fatal(err)
	}

	code, body, head := quoted(t, w.router, http.MethodGet, w.address(), "", "")
	if code != http.StatusOK {
		t.Fatalf("GET the row at revision %d = %d %s, want the read that hands out a tag", at, code, body)
	}
	tag := head.Get("ETag")
	if tag != `"9007199254740993"` {
		t.Fatalf("the read of a row at %d answers ETag %s, want \"9007199254740993\": a caller that is not given this count has none it can quote",
			at, tag)
	}

	code, body, _ = quoted(t, w.router, http.MethodPatch, w.address(), `{"title":"Written past the limit"}`, tag)
	if code != http.StatusOK {
		t.Fatalf("PATCH quoting the tag this row read (%s) = %d %s, want the write of a row that has not moved", tag, code, body)
	}
	if _, _, revision := w.readBack(t); revision != at+1 {
		t.Fatalf("the accepted write left the row at revision %d, want %d", revision, at+1)
	}
}
