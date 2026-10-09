package main

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/modules/notification"
)

// The whole flow through the review page's own forms: a proposal is approved on the
// page, the page then offers Apply and nothing else that decides, and the Apply the
// page posts is the write that moves the task — once, to the next revision.
func TestTheReviewPageAppliesAnApprovedTaskChange(t *testing.T) {
	cfg, c, _, _ := taskChangeFixture(t, true)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	box, ok := c.mail.(*notification.Mailbox)
	if !ok {
		t.Fatalf("the composition wired %T as its mailer, want the mailbox", c.mail)
	}
	reviewer := decider(t, cfg, admin, box, "page-reviewer")
	id := newTask(t, cfg, admin, "Inspect a valve", "normal")
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, proposalsPath,
		`{"subjectModule":"task","subjectEntity":"task","subjectId":"`+id+`","diff":{"priority":"high"},"summary":"Inspect sooner"}`)
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("propose = %d %s", code, body)
	}
	pid := field(t, body, "id")
	if code, page := postPageForm(t, cfg, reviewer, "/app/change/proposals/"+pid+"/review",
		"verdict=approved&expectedRevision=1"); code != http.StatusSeeOther {
		t.Fatalf("approve on the page = %d, want 303: %s", code, page)
	}

	status, _, page := askNavigate(t, cfg, reviewer, acmeHost, "/app/change/proposals/"+pid)
	if status != http.StatusOK {
		t.Fatalf("the approved proposal's page = %d", status)
	}
	if !strings.Contains(page, `/app/change/proposals/`+pid+`/apply"`) {
		t.Fatalf("the approved proposal's page offers no Apply form: %s", page)
	}
	if strings.Contains(page, `/app/change/proposals/`+pid+`/review"`) {
		t.Errorf("the approved proposal's page still offers a verdict")
	}

	if code, page := postPageForm(t, cfg, reviewer, "/app/change/proposals/"+pid+"/apply",
		"expectedRevision=2"); code != http.StatusSeeOther {
		t.Fatalf("apply on the page = %d, want 303: %s", code, page)
	}
	code, body = do(t, cfg, admin, http.MethodGet, acmeHost, tasksPath+"/"+id, "")
	if code != http.StatusOK {
		t.Fatalf("read the task = %d %s", code, body)
	}
	if field(t, body, "priority") != "high" || fieldNumber(t, body, "revision") != 2 {
		t.Errorf("the applied change is not the task's next revision: %s", body)
	}
	code, body = do(t, cfg, admin, http.MethodGet, acmeHost, proposalsPath+"/"+pid, "")
	if code != http.StatusOK || field(t, body, "state") != "applied" {
		t.Errorf("the proposal after apply = %d %s, want state applied", code, body)
	}
	// A second click on the same Apply is refused and writes nothing.
	if code, _ := postPageForm(t, cfg, reviewer, "/app/change/proposals/"+pid+"/apply",
		"expectedRevision=2"); code != http.StatusConflict {
		t.Errorf("a second apply on the page = %d, want 409", code)
	}
	_, body = do(t, cfg, admin, http.MethodGet, acmeHost, tasksPath+"/"+id, "")
	if fieldNumber(t, body, "revision") != 2 {
		t.Errorf("a second apply moved the task: %s", body)
	}
}

// postPageForm posts a form as the browser does and reports the answer without following it.
func postPageForm(t *testing.T, cfg config.Config, client *http.Client, path, form string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		"http://"+cfg.Server.Addr+path, strings.NewReader(form))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = acmeHost
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	noFollow := *client
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := noFollow.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var page strings.Builder
	if _, err := io.Copy(&page, res.Body); err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, page.String()
}
