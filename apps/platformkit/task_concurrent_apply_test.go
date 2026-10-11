package main

import (
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/modules/notification"
)

// Two approved proposals over one task, made at the same revision, applied at the same
// moment: the task's row lock decides, one apply writes, the other is told its base is
// stale, and the task's revision moves exactly once.
func TestTwoApprovedTaskProposalsAppliedAtOnceWriteOnce(t *testing.T) {
	cfg, c, _, _ := taskChangeFixture(t, true)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	box, ok := c.mail.(*notification.Mailbox)
	if !ok {
		t.Fatalf("the composition wired %T as its mailer, want the mailbox", c.mail)
	}
	grace := decider(t, cfg, admin, box, "grace")
	id := newTask(t, cfg, admin, "Two hands on one deadline", "normal")

	var pids []string
	for _, diff := range []string{`{"slaDeadline":"2030-06-01T00:00:00Z"}`, `{"priority":"critical"}`} {
		code, body := do(t, cfg, admin, http.MethodPost, acmeHost, proposalsPath,
			`{"subjectModule":"task","subjectEntity":"task","subjectId":"`+id+`","diff":`+diff+`,"summary":"one of two"}`)
		if code != http.StatusOK && code != http.StatusCreated {
			t.Fatalf("POST %s = %d %s", proposalsPath, code, body)
		}
		pid := field(t, body, "id")
		if code, body = do(t, cfg, grace, http.MethodPost, acmeHost, proposalsPath+"/"+pid+"/review",
			`{"verdict":"approved","expectedRevision":1}`); code != http.StatusOK {
			t.Fatalf("review = %d %s", code, body)
		}
		pids = append(pids, pid)
	}

	codes := make([]int, len(pids))
	var wg sync.WaitGroup
	for i, pid := range pids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i], _ = do(t, cfg, grace, http.MethodPost, acmeHost, proposalsPath+"/"+pid+"/apply", `{"expectedRevision":2}`)
		}()
	}
	wg.Wait()
	won, stale := 0, 0
	for _, code := range codes {
		switch code {
		case http.StatusOK:
			won++
		case http.StatusConflict:
			stale++
		}
	}
	if won != 1 || stale != 1 {
		t.Errorf("two applies at once answered %v, want one 200 and one 409", codes)
	}
	if _, body := do(t, cfg, admin, http.MethodGet, acmeHost, tasksPath+"/"+id, ""); fieldNumber(t, body, "revision") != 2 {
		t.Errorf("the task moved more than once: %s", body)
	}
}

// A second tenant's administrator cannot write the first tenant's task through the
// direct door either: the PATCH answers 404 and the row and its revision are as they were.
func TestAnotherTenantCannotPatchATask(t *testing.T) {
	cfg, _, _, _ := taskChangeFixture(t, false)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	task := newTask(t, cfg, admin, "Acme's own", "normal")
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"globex","name":"Globex","host":"`+globexHost+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s", tenantPath, code, body)
	}
	provision(t, cfg, uuid.MustParse(field(t, body, "id")), "root@globex.localhost")
	globex := signIn(t, cfg, globexHost, "root@globex.localhost", adminPass)

	if code, body = do(t, cfg, globex, http.MethodPatch, globexHost, tasksPath+"/"+task, `{"title":"globex was here"}`); code != http.StatusNotFound {
		t.Errorf("globex PATCH of acme's task = %d %s, want 404", code, body)
	}
	if _, body = do(t, cfg, admin, http.MethodGet, acmeHost, tasksPath+"/"+task, ""); fieldNumber(t, body, "revision") != 1 {
		t.Errorf("acme's task moved: %s", body)
	}
}
