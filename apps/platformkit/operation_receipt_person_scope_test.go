package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

// An operation's receipt is read by the person who started it: a second
// administrator of the same tenant, holding the receipt's address, is answered
// as if no such operation existed, while its author still reads it.
func TestAnOperationReceiptIsReadOnlyByThePersonWhoStartedIt(t *testing.T) {
	cfg, mods, fixture := deviceComposition(t)
	install(t, fixture.path)
	start(t, cfg, mods, fixture.opts)
	const otherEmail = "colleague@acme.localhost"
	provision(t, cfg, acmeTenant(t, cfg), otherEmail)
	author := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	colleague := signIn(t, cfg, acmeHost, otherEmail, adminPass)

	req, err := http.NewRequest(http.MethodPost, "http://"+cfg.Server.Addr+tasksPath, strings.NewReader(`{"title":"authored task"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = acmeHost
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(httpx.IdempotencyKeyHeader, "offline.task.authored")
	res, err := author.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	location := res.Header.Get(httpx.OperationLocationHeader)
	if res.StatusCode != http.StatusCreated || location == "" {
		t.Fatalf("create = %d, receipt %q; want 201 with a receipt", res.StatusCode, location)
	}
	if code, body := do(t, cfg, author, http.MethodGet, acmeHost, location, ""); code != http.StatusOK || !strings.Contains(body, `"outcome":"succeeded"`) {
		t.Errorf("author reads the receipt = %d %s, want 200 succeeded", code, body)
	}
	if code, body := do(t, cfg, colleague, http.MethodGet, acmeHost, location, ""); code != http.StatusNotFound {
		t.Errorf("colleague reads the author's receipt = %d %s, want 404", code, body)
	}
}
