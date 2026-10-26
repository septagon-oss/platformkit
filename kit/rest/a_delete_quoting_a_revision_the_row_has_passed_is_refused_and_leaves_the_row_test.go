package rest_test

// The delete door's tag rule, asked at the counts a client actually reaches: a tag the
// row has passed refuses the delete, and the refusal leaves the row, its write count
// and its kept versions exactly as they were — the only thing between a stale editor
// and a removed row.

import (
	"net/http"
	"testing"
)

func TestADeleteQuotingARevisionTheRowHasPassedIsRefusedAndLeavesTheRow(t *testing.T) {
	w := memos(t)
	code, body, head := quoted(t, w.router, http.MethodGet, w.address(), "", "")
	if code != http.StatusOK || head.Get("ETag") != `"1"` {
		t.Fatalf("GET the row = %d %s, want 200 with the tag \"1\" the delete will quote", code, body)
	}
	stale := head.Get("ETag")
	if code, body := w.edit(t, "A write after the read"); code != http.StatusOK {
		t.Fatalf("PATCH that moves the row = %d %s, want 200 before the conditional delete is asked", code, body)
	}
	versions := len(w.kept.kept)
	if code, body, _ := quoted(t, w.router, http.MethodDelete, w.address(), "", stale); code != http.StatusPreconditionFailed {
		t.Fatalf("DELETE quoting the stale %s after the row moved = %d %s, want 412: a stale tag may not remove a newer row", stale, code, body)
	}
	if title, _, revision := w.readBack(t); title != "A write after the read" || revision != 2 {
		t.Fatalf("the refused delete left %q at revision %d, want the newer write at 2", title, revision)
	}
	if len(w.kept.kept) != versions {
		t.Fatalf("the refused delete recorded %d kept versions, want the %d the writes it refused to undo already left", len(w.kept.kept), versions)
	}
}
