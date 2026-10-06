package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/notification"
)

// The person who proposed a change is told what was decided about it: one notice,
// in their own list, once the second account has given its verdict.
func TestTheProposerIsToldTheVerdictOnce(t *testing.T) {
	cfg, c, _, _ := taskChangeFixture(t, true)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	box, ok := c.mail.(*notification.Mailbox)
	if !ok {
		t.Fatalf("the composition wired %T as its mailer, want the mailbox", c.mail)
	}
	grace := decider(t, cfg, admin, box, "grace")
	id := newTask(t, cfg, admin, "Recalibrate the meters", "normal")

	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, proposalsPath,
		`{"subjectModule":"task","subjectEntity":"task","subjectId":"`+id+`",`+
			`"diff":{"priority":"high"},"summary":"the meters drift"}`)
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s", proposalsPath, code, body)
	}
	pid := field(t, body, "id")
	if code, body = do(t, cfg, grace, http.MethodPost, acmeHost, proposalsPath+"/"+pid+"/review",
		`{"verdict":"declined","comment":"not this quarter","expectedRevision":1}`); code != http.StatusOK {
		t.Fatalf("review = %d %s", code, body)
	}
	eventually(t, "the proposer's notice of the verdict", func() bool {
		code, body := do(t, cfg, admin, http.MethodGet, acmeHost, noticePath, "")
		return code == http.StatusOK && strings.Contains(body, `"total":1`)
	})
}
