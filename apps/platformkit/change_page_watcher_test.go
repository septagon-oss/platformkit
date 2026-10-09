package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/modules/notification"
)

// An account that may only read proposals is drawn no Apply, and the Apply door the
// page would have drawn refuses it: the control and the route ask the same grant.
func TestAWatcherCannotApplyThroughTheReviewPage(t *testing.T) {
	cfg, c, _, _ := taskChangeFixture(t, true)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	box, ok := c.mail.(*notification.Mailbox)
	if !ok {
		t.Fatalf("the composition wired %T as its mailer, want the mailbox", c.mail)
	}
	reviewer := decider(t, cfg, admin, box, "watch-reviewer")
	if code, body := do(t, cfg, admin, http.MethodPut, acmeHost, rolesPath+"/watcher",
		`{"permissions":["change:read","task:read"]}`); code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("PUT %s/watcher = %d %s", rolesPath, code, body)
	}
	watcher := invitedAs(t, cfg, admin, box, "watcher-one", "watcher")
	id := newTask(t, cfg, admin, "Inspect a valve", "normal")
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, proposalsPath,
		`{"subjectModule":"task","subjectEntity":"task","subjectId":"`+id+`","diff":{"priority":"high"},"summary":"Inspect sooner"}`)
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("propose = %d %s", code, body)
	}
	pid := field(t, body, "id")
	if code, page := postPageForm(t, cfg, reviewer, "/app/change/proposals/"+pid+"/review",
		"verdict=approved&expectedRevision=1"); code != http.StatusSeeOther {
		t.Fatalf("approve on the page = %d: %s", code, page)
	}
	status, _, page := askNavigate(t, cfg, watcher, acmeHost, "/app/change/proposals/"+pid)
	if status != http.StatusOK || !strings.Contains(page, "Inspect sooner") {
		t.Fatalf("the watcher's read of the proposal = %d", status)
	}
	if strings.Contains(page, "/apply\"") {
		t.Errorf("a watcher is drawn the Apply control")
	}
	if code, _ := postPageForm(t, cfg, watcher, "/app/change/proposals/"+pid+"/apply", "expectedRevision=2"); code != http.StatusForbidden {
		t.Errorf("a watcher's Apply = %d, want 403", code)
	}
	_, body = do(t, cfg, admin, http.MethodGet, acmeHost, tasksPath+"/"+id, "")
	if fieldNumber(t, body, "revision") != 1 || field(t, body, "priority") != "normal" {
		t.Errorf("a watcher's Apply moved the task: %s", body)
	}
}

// invitedAs makes one account holding one named role, the way decider does.
func invitedAs(t *testing.T, cfg config.Config, admin *http.Client, box *notification.Mailbox, key, role string) *http.Client {
	t.Helper()
	email := key + "@acme.localhost"
	if code, body := do(t, cfg, admin, http.MethodPost, acmeHost, invitesPath,
		`{"email":"`+email+`","displayName":"`+key+`","roles":["`+role+`"]}`); code != http.StatusCreated {
		t.Fatalf("POST %s for %s = %d %s", invitesPath, key, code, body)
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
		t.Fatalf("the reset for %s = %d %s", key, code, body)
	}
	return signIn(t, cfg, acmeHost, email, pass)
}
