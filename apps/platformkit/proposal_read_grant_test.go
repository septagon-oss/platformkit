package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/notification"
)

func TestProposalCommandsDoNotRevealAnotherPersonsRowWithoutReadGrant(t *testing.T) {
	cfg, c, _, _ := changeFixture(t, false)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	if code, body := do(t, cfg, admin, http.MethodPut, acmeHost, rolesPath+"/changesubmitonly",
		`{"permissions":["change:propose"]}`); code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("create submit-only role = %d %s", code, body)
	}
	const email = "change-submit-only@acme.localhost"
	if code, body := do(t, cfg, admin, http.MethodPost, acmeHost, invitesPath,
		`{"email":"`+email+`","displayName":"Submitter","roles":["changesubmitonly"]}`); code != http.StatusCreated {
		t.Fatalf("invite submitter = %d %s", code, body)
	}
	box, ok := c.mail.(*notification.Mailbox)
	if !ok {
		t.Fatalf("mailer is %T, want mailbox", c.mail)
	}
	var link string
	eventually(t, "the submitter's invitation", func() bool {
		for _, sent := range box.Sent() {
			if sent.To == email {
				link = sent.Body
				return true
			}
		}
		return false
	})
	if code, body := do(t, cfg, nil, http.MethodPost, acmeHost, "/api/v1/auth/password/reset",
		`{"token":"`+tokenIn(t, link)+`","new":"a chosen passphrase for submitter"}`); code != http.StatusOK {
		t.Fatalf("reset submitter password = %d %s", code, body)
	}
	submitter := signIn(t, cfg, acmeHost, email, "a chosen passphrase for submitter")

	const target = `{"subjectModule":"site","subjectEntity":"settings",` +
		`"subjectId":"00000000-0000-0000-0000-000000000000",` +
		`"diff":{"title":"Acme, one known change"},"summary":"`
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, proposalsPath,
		target+`the administrator's private reason"}`)
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("administrator's proposal = %d %s", code, body)
	}
	id := field(t, body, "id")
	if code, body := do(t, cfg, submitter, http.MethodGet, acmeHost, proposalsPath+"/"+id, ""); code != http.StatusForbidden {
		t.Fatalf("a submitter without read permission reads the proposal = %d %s, want 403", code, body)
	}

	code, body = do(t, cfg, submitter, http.MethodPost, acmeHost, proposalsPath,
		target+`the submitter's different reason"}`)
	if code != http.StatusConflict || strings.Contains(body, id) || strings.Contains(body, "private reason") {
		t.Errorf("repeating a known diff returned %d %s; want a conflict with no other person's row", code, body)
	}
	if code, body := do(t, cfg, admin, http.MethodPost, acmeHost, proposalsPath+"/"+id+"/withdraw",
		`{"expectedRevision":1}`); code != http.StatusOK {
		t.Fatalf("author withdraws proposal = %d %s", code, body)
	}
	if code, body := do(t, cfg, submitter, http.MethodPost, acmeHost, proposalsPath+"/"+id+"/withdraw",
		`{"expectedRevision":2}`); code != http.StatusConflict || strings.Contains(body, "private reason") {
		t.Errorf("another actor's withdraw retry returned %d %s; want a conflict with no proposal", code, body)
	}
}
