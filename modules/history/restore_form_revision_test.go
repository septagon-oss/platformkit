package history_test

import (
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/rest"
)

func TestAContentRestoreFormRefusesAStaleRevision(t *testing.T) {
	w, router, created, screen := contentHistoryFixture(t)
	before := w.versionsOf(created.ID)
	var originalBody string
	var originalRevision int64
	if err := w.admin.QueryRowContext(t.Context(),
		"SELECT body, revision FROM contents WHERE id = $1", created.ID).Scan(&originalBody, &originalRevision); err != nil {
		t.Fatal(err)
	}
	if originalRevision != 2 {
		t.Fatalf("the edit left revision %d, want 2", originalRevision)
	}
	rec := contentRestoreForm(t, router, created, screen, "version=1&expectedRevision=1", "en")
	if rec.Code != http.StatusPreconditionFailed {
		t.Errorf("stale restore form = %d, want 412", rec.Code)
	}
	var body string
	var revision int64
	if err := w.admin.QueryRowContext(t.Context(),
		"SELECT body, revision FROM contents WHERE id = $1", created.ID).Scan(&body, &revision); err != nil {
		t.Fatal(err)
	}
	if body != originalBody || revision != originalRevision {
		t.Errorf("stale restore form changed the body to %q on revision %d, want %q on %d", body, revision, originalBody, originalRevision)
	}
	after := w.versionsOf(created.ID)
	if len(after) != len(before) {
		t.Errorf("stale restore form left %d versions, want %d", len(after), len(before))
	}
	if n := w.outbox(rest.RestoredEvent); n != 0 {
		t.Errorf("stale restore form emitted %d restored events, want none", n)
	}
}
