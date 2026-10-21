package history_test

import (
	"net/http"
	"testing"
)

func TestARestoreRefusalKeepsAtMostTwoTextAlignments(t *testing.T) {
	_, router, created, screen := contentHistoryFixture(t, true)
	rec := contentRestoreForm(t, router, created, screen, "version=0&expectedRevision=2", "en")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid restore = %d %s, want 422", rec.Code, rec.Body.String())
	}
	measured := measureHistoryPage(t, rec.Body.String())
	if len(measured.Edges) > 2 {
		t.Errorf("restore refusal has %d text alignments %v, want at most two", len(measured.Edges), measured.Edges)
	}
}
