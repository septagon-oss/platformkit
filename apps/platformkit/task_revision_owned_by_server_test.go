package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/notification"
)

// A task's revision is the number a proposal's stale-base rule compares, so only the
// server may move it. A PATCH body that names it must not be able to wind the row back
// to the number an approved proposal was made against: if it could, a change approved
// against one deadline lands over a row somebody has since rewritten.
func TestAPatchCannotWindATaskBackToAStaleProposalsRevision(t *testing.T) {
	cfg, c, _, _ := taskChangeFixture(t, true)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	box, ok := c.mail.(*notification.Mailbox)
	if !ok {
		t.Fatalf("the composition wired %T as its mailer, want the mailbox", c.mail)
	}
	grace := decider(t, cfg, admin, box, "grace")
	id := newTask(t, cfg, admin, "Re-plan the boiler", "high")

	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, proposalsPath,
		`{"subjectModule":"task","subjectEntity":"task","subjectId":"`+id+`",`+
			`"diff":{"slaDeadline":"2030-06-01T00:00:00Z"},"summary":"move the boiler SLA"}`)
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s", proposalsPath, code, body)
	}
	pid := field(t, body, "id")
	if code, body = do(t, cfg, grace, http.MethodPost, acmeHost, proposalsPath+"/"+pid+"/review",
		`{"verdict":"approved","expectedRevision":1}`); code != http.StatusOK {
		t.Fatalf("review = %d %s", code, body)
	}

	// The row moves after the diff was made: the proposal is now stale.
	if code, body = do(t, cfg, admin, http.MethodPatch, acmeHost, tasksPath+"/"+id,
		`{"title":"Re-plan the boiler, and the flue"}`); code != http.StatusOK {
		t.Fatalf("PATCH title = %d %s", code, body)
	}
	// A body naming the revision. Whether the door refuses it or ignores it, the row
	// must not end up on a number it has already been on.
	code, body = do(t, cfg, admin, http.MethodPatch, acmeHost, tasksPath+"/"+id,
		`{"description":"winding back","revision":0}`)
	t.Logf("PATCH with a revision in the body = %d %s", code, body)
	_, body = do(t, cfg, admin, http.MethodGet, acmeHost, tasksPath+"/"+id, "")
	if got := fieldNumber(t, body, "revision"); got < 2 {
		t.Errorf("the task is on revision %v after a write named it; the server owns that number", got)
	}

	if code, body = do(t, cfg, grace, http.MethodPost, acmeHost, proposalsPath+"/"+pid+"/apply",
		`{"expectedRevision":2}`); code != http.StatusConflict {
		t.Errorf("apply of a proposal over a row rewritten since = %d %s, want 409 stale", code, body)
	}
	_, body = do(t, cfg, admin, http.MethodGet, acmeHost, tasksPath+"/"+id, "")
	if strings.Contains(body, "2030-06-01") {
		t.Errorf("the stale proposal's deadline reached the row: %s", body)
	}
}
