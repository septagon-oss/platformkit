package main

// A resource without a write closure declares no form, so even the administrator, who holds
// every grant, is answered 404 at its New and Edit addresses; the writable task list's New
// stays a form for the same session. The grant is not what separates the two answers.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// TestAReadOnlyResourceHasNoFormEvenForTheAdministrator asks the administrator for the
// delivery log's form addresses and for the task list's New.
func TestAReadOnlyResourceHasNoFormEvenForTheAdministrator(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	const ranking = "en-GB,en;q=0.9"

	status, ctype, _, html := r7navigate(t, cfg, admin, acmeHost, "/app/webhook/deliveries", ranking)
	if status != http.StatusOK || !strings.HasPrefix(ctype, "text/html") {
		t.Fatalf("GET /app/webhook/deliveries = %d %q, want the list: %s", status, ctype, firstLineOf(html))
	}
	for _, at := range []string{
		"/app/webhook/deliveries/new",
		"/app/webhook/deliveries/" + uuid.NewString() + "/edit",
	} {
		if status, _, _, html := r7navigate(t, cfg, admin, acmeHost, at, ranking); status != http.StatusNotFound {
			t.Errorf("GET %s for the administrator = %d, want 404: %s", at, status, firstLineOf(html))
		}
	}
	if status, _, _, html := r7navigate(t, cfg, admin, acmeHost, "/app/task/tasks/new", ranking); status != http.StatusOK {
		t.Errorf("GET /app/task/tasks/new for the administrator = %d, want the form: %s", status, firstLineOf(html))
	}
}
