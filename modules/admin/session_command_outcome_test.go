package admin_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestEachSessionCommandHasItsOwnOutcomeRegion(t *testing.T) {
	router := mountAs(t, caller{}, withSessions(threeSessions()))
	res := signedInAs(t, router, http.MethodGet, "/app/auth/sessions", "")
	if res.Code != http.StatusOK {
		t.Fatalf("sessions returned %d: %s", res.Code, res.Body)
	}
	body := res.Body.String()
	// Optional snapshots let the same existing page fixture feed the design gate.
	if dir := os.Getenv("PLATFORMKIT_COMMAND_DESIGN_DIR"); dir != "" {
		_, sheet, _ := callAt(t, router, host, http.MethodGet, "/app/admin/assets/app.css", "")
		body = strings.ReplaceAll(body, "/app/admin/assets/app.css", "app.css")
		for name, content := range map[string]string{"sessions.html": body, "app.css": sheet} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	attr := func(n *html.Node, key string) string {
		for _, a := range n.Attr {
			if a.Key == key {
				return a.Val
			}
		}
		return ""
	}
	var containsOutcome func(*html.Node) bool
	containsOutcome = func(n *html.Node) bool {
		if attr(n, "role") == "status" || attr(n, "role") == "alert" || attr(n, "aria-live") != "" {
			return true
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if containsOutcome(c) {
				return true
			}
		}
		return false
	}
	commands := 0
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Data == "form" && attr(n, "hx-ext") == "command" {
			commands++
			if !containsOutcome(n) {
				t.Errorf("command form %q at %s has no outcome region of its own", attr(n, "id"), attr(n, "action"))
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(doc)
	if commands == 0 {
		t.Fatal("sessions exposes no command forms")
	}
}
