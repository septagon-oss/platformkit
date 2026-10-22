package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// TestATaskPageThatFindsNoRowNamesTheItemAndLinksBackToTheList: the generated record page of
// a task that does not exist — and of a task that exists in another tenant — says the item
// is no longer available and links to the task list, and shows nothing of the other
// tenant's row.
func TestATaskPageThatFindsNoRowNamesTheItemAndLinksBackToTheList(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	const title = "Renew the harbour lease"
	task := newTask(t, cfg, admin, title, "normal")
	if code, _, body := askNavigate(t, cfg, admin, acmeHost, "/app/task/tasks/"+task); code != http.StatusOK || !strings.Contains(body, title) {
		t.Fatalf("acme's own task page = %d; this case is about the page that finds no row: %s", code, trimHTML(body))
	}

	secondTenant(t, cfg, "globex", "root@globex.localhost")
	globex := signIn(t, cfg, globexHost, "root@globex.localhost", adminPass)
	for _, at := range []struct {
		who    string
		client *http.Client
		host   string
		id     string
	}{
		{"acme, a task id nobody wrote", admin, acmeHost, uuid.NewString()},
		{"globex, acme's task id", globex, globexHost, task},
	} {
		code, _, body := askNavigate(t, cfg, at.client, at.host, "/app/task/tasks/"+at.id)
		if code != http.StatusNotFound {
			t.Errorf("%s: the task page = %d, want 404", at.who, code)
		}
		for _, want := range []string{"This item is no longer available.", `href="/app/task/tasks"`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: the task page omits %q: %s", at.who, want, trimHTML(body))
			}
		}
		for _, reject := range []string{title, "Not Found"} {
			if strings.Contains(body, reject) {
				t.Errorf("%s: the task page says %q: %s", at.who, reject, trimHTML(body))
			}
		}
	}
}
