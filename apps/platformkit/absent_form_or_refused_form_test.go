package main

// A form's address has two answers short of the form, and they mean different things: 403 says
// the resource has the form and this person may not use it; 404 says the resource declares no
// such verb, whoever asks. The case asks one reader who holds every read and no write, so both
// answers come from one session: the writable task list's New is refused, the delivery log's
// New and Edit and the endpoint list's New are not found, and the endpoint's Edit — a verb the
// resource does declare — is refused again.
//
// Reachability runs through the fixed behaviour: the reader is served each list first, so a 404
// below is an absent form and a 403 is the write grant, never a list the reader cannot see.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/modules/notification"
)

// TestAFormThePersonMayNotUseIsRefusedAndAFormNobodyDeclaredIsNotFound signs in a reader of
// tasks and webhooks who holds neither write grant and asks every form address of the three
// resources.
func TestAFormThePersonMayNotUseIsRefusedAndAFormNobodyDeclaredIsNotFound(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	box, ok := c.mail.(*notification.Mailbox)
	if !ok {
		t.Fatalf("the reference application mails through %T", c.mail)
	}
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	if code, body := do(t, cfg, admin, http.MethodPut, acmeHost, rolesPath+"/reader",
		`{"permissions":["task:read","webhook:read"]}`); code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("PUT %s/reader = %d %s, want the role", rolesPath, code, body)
	}
	reader := invitedAs(t, cfg, admin, box, "reader", "reader")
	const ranking = "en-GB,en;q=0.9"

	// Rows that exist, so an Edit refused below is refused for its grant and not for its row.
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, tasksPath, `{"title":"A task with a form"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", tasksPath, code, body)
	}
	task := field(t, body, "id")
	code, body = do(t, cfg, admin, http.MethodPost, acmeHost, "/api/v1/webhook/endpoints",
		`{"name":"A receiver","url":"https://hooks.example.test/tasks","events":["task.task.created"]}`)
	if code != http.StatusCreated {
		t.Fatalf("register an endpoint = %d %s, want 201", code, body)
	}
	endpoint := createdRowID(t, body, "endpoint")

	for _, list := range []string{"/app/task/tasks", "/app/webhook/deliveries", "/app/webhook/endpoints"} {
		status, ctype, _, html := r7navigate(t, cfg, reader, acmeHost, list, ranking)
		if status != http.StatusOK || !strings.HasPrefix(ctype, "text/html") {
			t.Fatalf("GET %s for the reader = %d %q, want the list: %s", list, status, ctype, firstLineOf(html))
		}
	}

	forms := []struct {
		at   string
		want int
	}{
		{"/app/task/tasks/new", http.StatusForbidden},
		{"/app/task/tasks/" + task + "/edit", http.StatusForbidden},
		{"/app/webhook/deliveries/new", http.StatusNotFound},
		{"/app/webhook/deliveries/" + uuid.NewString() + "/edit", http.StatusNotFound},
		{"/app/webhook/endpoints/new", http.StatusNotFound},
		{"/app/webhook/endpoints/" + endpoint + "/edit", http.StatusForbidden},
	}
	for _, form := range forms {
		status, ctype, _, html := r7navigate(t, cfg, reader, acmeHost, form.at, ranking)
		if status != form.want {
			t.Errorf("GET %s for a reader without the write grant = %d, want %d: %s",
				form.at, status, form.want, firstLineOf(html))
			continue
		}
		if !strings.HasPrefix(ctype, "text/html") ||
			!strings.HasPrefix(strings.ToLower(strings.TrimSpace(html)), "<!doctype html") {
			t.Errorf("GET %s answered %d as %q, want the shell's page: %s", form.at, status, ctype, firstLineOf(html))
		}
	}
}
