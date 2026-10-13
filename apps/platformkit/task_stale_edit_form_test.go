package main

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func TestAStaleTaskEditFormCannotOverwriteANewerWrite(t *testing.T) {
	cfg, _, _, _ := taskChangeFixture(t, false)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	id := newTask(t, cfg, admin, "Original title", "normal")
	path := "/app/task/tasks/" + id
	status, _, page := askNavigate(t, cfg, admin, acmeHost, path+"/edit")
	if status != http.StatusOK || !strings.Contains(page, `name="title"`) {
		t.Fatalf("edit form status=%d; title control present=%v", status, strings.Contains(page, `name="title"`))
	}
	// Carry the original form's hidden concurrency and security values, including
	// any revision token the renderer supplies. Never fetch a newer form before
	// sending this edit: the person still has the first revision on their screen.
	values := url.Values{"title": {"Older editor's replacement"}}
	attributes := regexp.MustCompile(`([a-zA-Z]+)="([^"]*)"`)
	for _, input := range regexp.MustCompile(`<input\b[^>]*>`).FindAllString(page, -1) {
		attrs := map[string]string{}
		for _, attr := range attributes.FindAllStringSubmatch(input, -1) {
			attrs[attr[1]] = html.UnescapeString(attr[2])
		}
		if attrs["type"] == "hidden" && attrs["name"] != "" {
			values.Add(attrs["name"], attrs["value"])
		}
	}
	code, body := do(t, cfg, admin, http.MethodPatch, acmeHost, tasksPath+"/"+id,
		`{"title":"Newer editor's committed title"}`)
	if code != http.StatusOK || fieldNumber(t, body, "revision") != 2 {
		t.Fatalf("concurrent edit = %d %s", code, body)
	}
	status, _, _ = askNavigateRaw(t, cfg, admin, http.MethodPost, acmeHost, path,
		"application/x-www-form-urlencoded", values.Encode())
	code, body = do(t, cfg, admin, http.MethodGet, acmeHost, tasksPath+"/"+id, "")
	if code != http.StatusOK {
		t.Fatalf("read task after stale form = %d %s", code, body)
	}
	if field(t, body, "title") != "Newer editor's committed title" || fieldNumber(t, body, "revision") != 2 {
		t.Errorf("stale form returned %d and overwrote the newer edit: %s", status, body)
	}
}
