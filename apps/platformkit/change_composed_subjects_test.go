package main

import (
	"net/http"
	"strings"
	"testing"
)

// Product and access contribute different subjects. Both must reach the service
// behind the API and the review pages when the reference application resolves.
func TestTheReviewQueueSharesBothContributedSubjectsWithTheAPI(t *testing.T) {
	cfg, _, _, _ := taskChangeFixture(t, true)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	id := newTask(t, cfg, admin, "Inspect the cooling loop", "normal")
	var proposals []string
	for _, input := range []struct {
		body    string
		summary string
	}{
		{`{"subjectModule":"site","subjectEntity":"settings","subjectId":"00000000-0000-0000-0000-000000000000","diff":{"title":"New site title"},"summary":"Rename the shared site"}`, "Rename the shared site"},
		{`{"subjectModule":"task","subjectEntity":"task","subjectId":"` + id + `","diff":{"priority":"high"},"summary":"Inspect the loop sooner"}`, "Inspect the loop sooner"},
	} {
		code, body := do(t, cfg, admin, http.MethodPost, acmeHost, proposalsPath, input.body)
		if code != http.StatusOK && code != http.StatusCreated {
			t.Fatalf("propose %q = %d %s", input.summary, code, body)
		}
		proposal := field(t, body, "id")
		proposals = append(proposals, proposal)
		status, _, page := askNavigate(t, cfg, admin, acmeHost, pinnedProposals+"/"+proposal)
		if status != http.StatusOK || !strings.Contains(page, input.summary) {
			t.Fatalf("proposal page %s = %d; missing %q", proposal, status, input.summary)
		}
	}
	code, body := do(t, cfg, admin, http.MethodGet, acmeHost, proposalsPath, "")
	if code != http.StatusOK || fieldNumber(t, body, "total") != 2 {
		t.Fatalf("proposal API = %d %s; want both contributed subjects", code, body)
	}
	status, _, page := askNavigate(t, cfg, admin, acmeHost, pinnedProposals)
	if status != http.StatusOK {
		t.Fatalf("review queue = %d", status)
	}
	for _, proposal := range proposals {
		if !strings.Contains(body, proposal) || !strings.Contains(page, pinnedProposals+"/"+proposal) {
			t.Errorf("proposal %s is not shared by the API and review queue", proposal)
		}
	}
}
