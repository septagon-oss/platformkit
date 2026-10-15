package main

// Proposing and deciding are two grants, and the four-eyes rule is only worth
// something if the second one is a grant somebody has to be given. An account that
// may read and propose — and nothing else — reads somebody else's proposal, puts one
// of its own forward, and is refused at the review and the apply doors with the
// status that means "not yours to do", leaving the proposal where it was.

import (
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/modules/notification"
)

const proposerOnlyEmail = "proposer@acme.localhost"

func TestAnAccountThatMayOnlyProposeCannotDecide(t *testing.T) {
	cfg, c, _, _ := changeFixture(t, false)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	if code, body := do(t, cfg, admin, http.MethodPut, acmeHost, rolesPath+"/proposer",
		`{"permissions":["change:read","change:propose"]}`); code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("PUT %s/proposer = %d %s, want the role", rolesPath, code, body)
	}
	if code, body := do(t, cfg, admin, http.MethodPost, acmeHost, invitesPath,
		`{"email":"`+proposerOnlyEmail+`","displayName":"Proposer","roles":["proposer"]}`); code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", invitesPath, code, body)
	}
	box, ok := c.mail.(*notification.Mailbox)
	if !ok {
		t.Fatalf("the composition wired %T as its mailer, want the mailbox", c.mail)
	}
	var link string
	eventually(t, "the proposer's invitation to be mailed", func() bool {
		for _, sent := range box.Sent() {
			if sent.To == proposerOnlyEmail {
				link = sent.Body
				return true
			}
		}
		return false
	})
	redeemMailedLink(t, cfg, nil, acmeHost, "/api/v1/auth/password/reset",
		`{"token":"`+tokenIn(t, link)+`","new":"a chosen passphrase for the proposer"}`)
	proposer := signIn(t, cfg, acmeHost, proposerOnlyEmail, "a chosen passphrase for the proposer")

	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, proposalsPath,
		`{"subjectModule":"site","subjectEntity":"settings",`+
			`"subjectId":"00000000-0000-0000-0000-000000000000",`+
			`"diff":{"title":"Acme, by the administrator"},"summary":"the administrator's change"}`)
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want the proposal recorded", proposalsPath, code, body)
	}
	id := field(t, body, "id")

	// The grants it does have answer: it reads the proposal and proposes its own.
	if code, body := do(t, cfg, proposer, http.MethodGet, acmeHost, proposalsPath+"/"+id, ""); code != http.StatusOK {
		t.Fatalf("the proposer reads %s/%s = %d %s, want 200", proposalsPath, id, code, body)
	}
	if code, body := do(t, cfg, proposer, http.MethodPost, acmeHost, proposalsPath,
		`{"subjectModule":"site","subjectEntity":"settings",`+
			`"subjectId":"00000000-0000-0000-0000-000000000000",`+
			`"diff":{"title":"Acme, by the proposer"},"summary":"the proposer's change"}`); code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("the proposer proposes = %d %s, want the proposal recorded", code, body)
	}

	// The grant it does not have refuses.
	if code, body := do(t, cfg, proposer, http.MethodPost, acmeHost, proposalsPath+"/"+id+"/review",
		`{"verdict":"approved","expectedRevision":1}`); code != http.StatusForbidden {
		t.Errorf("the proposer reviews somebody else's proposal = %d %s, want 403", code, body)
	}
	if code, body := do(t, cfg, proposer, http.MethodPost, acmeHost, proposalsPath+"/"+id+"/apply",
		`{"expectedRevision":1}`); code != http.StatusForbidden {
		t.Errorf("the proposer applies somebody else's proposal = %d %s, want 403", code, body)
	}
	code, body = do(t, cfg, admin, http.MethodGet, acmeHost, proposalsPath+"/"+id, "")
	if code != http.StatusOK {
		t.Fatalf("GET %s/%s = %d %s", proposalsPath, id, code, body)
	}
	if got := field(t, body, "state"); got != "proposed" {
		t.Errorf("the refused decisions left the proposal %q, want proposed", got)
	}
}
