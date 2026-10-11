package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// The review's stale-write defect, at the task door: a client that read the row at
// one revision and writes after somebody else moved it is told so, rather than
// overwriting the other write. The item read carries the row's tag, and a PATCH that
// quotes an old tag in If-Match answers 412 and writes nothing.
func TestAStaleIfMatchOnATaskPatchIsRefused(t *testing.T) {
	cfg, _, _, _ := taskChangeFixture(t, false)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	id := newTask(t, cfg, admin, "Replace the gaskets", "normal")

	send := func(method, body string, header map[string]string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(method, "http://"+cfg.Server.Addr+tasksPath+"/"+id, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Host = acmeHost
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		for k, v := range header {
			req.Header.Set(k, v)
		}
		resp, err := admin.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return resp
	}

	read := send(http.MethodGet, "", nil)
	tag := read.Header.Get("ETag")
	if read.StatusCode != http.StatusOK || tag == "" {
		t.Fatalf("GET the task = %d with ETag %q, want 200 and the row's tag", read.StatusCode, tag)
	}
	if code, body := do(t, cfg, admin, http.MethodPatch, acmeHost, tasksPath+"/"+id,
		`{"title":"Replace the gaskets, both pumps"}`); code != http.StatusOK {
		t.Fatalf("the other writer's PATCH = %d %s", code, body)
	}
	if stale := send(http.MethodPatch, `{"title":"Replace the gaskets"}`, map[string]string{"If-Match": tag}); stale.StatusCode != http.StatusPreconditionFailed {
		t.Errorf("PATCH quoting the tag read before the other write = %d, want 412", stale.StatusCode)
	}
	if _, body := do(t, cfg, admin, http.MethodGet, acmeHost, tasksPath+"/"+id, ""); !strings.Contains(body, "both pumps") {
		t.Errorf("the stale write overwrote the other one: %s", body)
	}
}
