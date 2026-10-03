package main

import (
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/modules/notification"
)

// TestAnApprovedProposalDoesNotAnswerASecondVerdictOverHTTP is the refusal at the
// door rather than in the service: an approved proposal has been decided, so a second
// verdict — from another account that may decide, or from the reviewer who changed
// their mind — is a refusal that leaves the row's verdict and reviewer exactly where
// the first decision put them. The module-level case
// (modules/change/internal/decided_verdict_test.go) holds the same rule one layer
// down; this one is here because the defect was reachable only through
// POST /api/v1/change/proposals/{id}/review, and a state machine pinned in a unit test
// while the door hands out the row is not pinned.
func TestAnApprovedProposalDoesNotAnswerASecondVerdictOverHTTP(t *testing.T) {
	cfg, c, _, _ := changeFixture(t, false)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	// A role with the whole of change control, so a second decider is refused by the
	// row and not by an absence of grants.
	if code, body := do(t, cfg, admin, http.MethodPut, acmeHost, rolesPath+"/decider",
		`{"permissions":["change:read","change:propose","change:decide"]}`); code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("PUT %s/decider = %d %s, want the role", rolesPath, code, body)
	}
	box, ok := c.mail.(*notification.Mailbox)
	if !ok {
		t.Fatalf("the composition wired %T as its mailer, want the mailbox", c.mail)
	}
	// One account, made the way the installation makes people: invited with the role,
	// given the passphrase the mailed link carries, signed in.
	decider := func(key string) *http.Client {
		t.Helper()
		email := key + "@acme.localhost"
		if code, body := do(t, cfg, admin, http.MethodPost, acmeHost, invitesPath,
			`{"email":"`+email+`","displayName":"`+key+`","roles":["decider"]}`); code != http.StatusCreated {
			t.Fatalf("POST %s for %s = %d %s, want 201", invitesPath, key, code, body)
		}
		var link string
		eventually(t, key+"'s invitation to be mailed", func() bool {
			for _, sent := range box.Sent() {
				if sent.To == email {
					link = sent.Body
					return true
				}
			}
			return false
		})
		pass := "a chosen passphrase for " + key
		if code, body := do(t, cfg, nil, http.MethodPost, acmeHost, "/api/v1/auth/password/reset",
			`{"token":"`+tokenIn(t, link)+`","new":"`+pass+`"}`); code != http.StatusOK {
			t.Fatalf("the reset for %s = %d %s, want 200", key, code, body)
		}
		return signIn(t, cfg, acmeHost, email, pass)
	}
	grace := decider("grace")
	heidi := decider("heidi")

	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, proposalsPath,
		`{"subjectModule":"site","subjectEntity":"settings",`+
			`"subjectId":"00000000-0000-0000-0000-000000000000",`+
			`"diff":{"title":"Acme, decided once"},"summary":"rename the site once"}`)
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want the proposal recorded", proposalsPath, code, body)
	}
	id := field(t, body, "id")
	if code, body = do(t, cfg, grace, http.MethodPost, acmeHost, proposalsPath+"/"+id+"/review",
		`{"verdict":"approved","comment":"yes","expectedRevision":1}`); code != http.StatusOK {
		t.Fatalf("the first verdict = %d %s, want 200", code, body)
	}
	if state := field(t, body, "state"); state != "approved" {
		t.Fatalf("the first verdict left the proposal %q, want approved", state)
	}

	// Three later verdicts, all refused: Heidi's agreement (the row is not hers to
	// confirm), Heidi's decline (an after-the-fact veto of a change the tenant already
	// approved) and Grace's reversal of their own decision.
	for _, try := range []struct {
		who     *http.Client
		verdict string
		why     string
	}{
		{heidi, "approved", "another decider confirming an approved proposal"},
		{heidi, "declined", "another decider vetoing an approved proposal"},
		{grace, "declined", "the reviewer reversing their own verdict"},
	} {
		if code, body := do(t, cfg, try.who, http.MethodPost, acmeHost, proposalsPath+"/"+id+"/review",
			`{"verdict":"`+try.verdict+`","expectedRevision":2}`); code != http.StatusConflict {
			t.Errorf("%s = %d %s, want 409 with the decision left where it was", try.why, code, body)
		}
	}
	// And the row says who decided, once: still approved at revision 2, still Grace's
	// verdict, read by an account entitled to read it.
	doc := mustGet(t, cfg, admin, proposalsPath+"/"+id)
	if state, verdict := field(t, doc, "state"), field(t, doc, "verdict"); state != "approved" || verdict != "approved" {
		t.Errorf("the later verdicts left the proposal %s/%s, want approved/approved: %s", state, verdict, doc)
	}
	if revision := fieldNumber(t, doc, "revision"); revision != 2 {
		t.Errorf("the later verdicts moved the row to revision %v, and a decided proposal does not move", revision)
	}
	if reviewer := field(t, doc, "reviewer"); reviewer == "" {
		t.Errorf("the decided proposal carries no reviewer in %s", doc)
	}

	// What an approved row does answer is the replay of the verdict on the record, by
	// the account that wrote it: the same row, no second decision, no third revision.
	if code, body = do(t, cfg, grace, http.MethodPost, acmeHost, proposalsPath+"/"+id+"/review",
		`{"verdict":"approved","expectedRevision":2}`); code != http.StatusOK {
		t.Fatalf("the reviewer's replay = %d %s, want 200 with the row as it stands", code, body)
	}
	if revision := fieldNumber(t, body, "revision"); revision != 2 {
		t.Errorf("the replay moved the decided row to revision %v, want 2", revision)
	}
}
