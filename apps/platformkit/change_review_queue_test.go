package main

import (
	"net/http"
	"strings"
	"testing"
)

// A reviewer has a page to review from: the queue lists an open proposal by its
// summary, for a member who may decide, on the app surface.
func TestTheReviewQueueListsAnOpenProposal(t *testing.T) {
	cfg, _, _, _ := taskChangeFixture(t, true)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	id := newTask(t, cfg, admin, "Swap the filters", "normal")
	if code, body := do(t, cfg, admin, http.MethodPost, acmeHost, proposalsPath,
		`{"subjectModule":"task","subjectEntity":"task","subjectId":"`+id+`",`+
			`"diff":{"priority":"high"},"summary":"filters clog in spring"}`); code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s", proposalsPath, code, body)
	}
	status, _, page := askNavigate(t, cfg, admin, acmeHost, "/app/change/proposals")
	if status != http.StatusOK || !strings.Contains(page, "filters clog in spring") {
		t.Errorf("GET /app/change/proposals = %d, want the queue with the open proposal in it", status)
	}
}
