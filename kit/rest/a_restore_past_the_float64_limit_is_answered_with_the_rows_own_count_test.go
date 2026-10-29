package rest_test

// A restore's condition is a count, and the row can be past what a float64 holds.
//
// The restore command takes the row's write count as the caller read it and refuses
// a row that has passed it, naming the count the row is on so the next attempt can
// quote it. Both halves of that promise have to keep their digits: the count in the
// request, or an honest caller is refused against a number the row never was; and
// the count the refusal hands back, or the retry it invites is refused again and the
// person is left with no count that works. This walks both legs over one row at
// 2^53+1 and asks nothing of a page — the same door the mobile flows command.

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestARestorePastTheFloat64LimitIsRefusedByTheRowsCountAndAnsweredByIt(t *testing.T) {
	w := memos(t)
	const at = 9007199254740993     // 2^53+1: the first write count no float64 holds
	const quoted = 9007199254740992 // 2^53: the last one it holds, and where a rounding falls
	if _, err := w.admin.ExecContext(t.Context(),
		`UPDATE rest_memos SET revision = $1 WHERE id = $2`, at, w.memo); err != nil {
		t.Fatal(err)
	}

	code, body := w.restore(t, 1, quoted)
	if code != http.StatusPreconditionFailed {
		t.Fatalf("POST restore quoting %d against the row at %d = %d, want the refusal of a row that moved: %s",
			quoted, at, code, body)
	}
	if !strings.Contains(body, strconv.FormatInt(at, 10)) {
		t.Fatalf("the refusal holds %q, want it to name the count the row counts itself at (%d), which is the only count the next restore can quote",
			body, at)
	}
	if _, _, revision := w.readBack(t); revision != at {
		t.Fatalf("the refused restore left the row at revision %d, want %d: a refusal writes nothing", revision, at)
	}

	if code, body := w.restore(t, 1, at); code != http.StatusOK {
		t.Fatalf("POST restore quoting the count that refusal named (%d) = %d, want the restore accepted: %s",
			at, code, body)
	}
	if _, _, revision := w.readBack(t); revision != at+1 {
		t.Fatalf("the accepted restore left the row at revision %d, want %d", revision, at+1)
	}
}
