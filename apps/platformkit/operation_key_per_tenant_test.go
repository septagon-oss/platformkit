package main

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

// One Idempotency-Key sent by two tenants is two operations: each tenant's write
// is created rather than replayed from the other's, each answer names its own
// receipt, and neither tenant can read the other's receipt.
func TestOneIdempotencyKeyInTwoTenantsIsTwoOperations(t *testing.T) {
	cfg, mods, fixture := deviceComposition(t)
	install(t, fixture.path)
	start(t, cfg, mods, fixture.opts)
	acme := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	code, body := do(t, cfg, acme, http.MethodPost, acmeHost, tenantPath, `{"slug":"globex","name":"Globex","host":"`+globexHost+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s", tenantPath, code, body)
	}
	provision(t, cfg, uuid.MustParse(field(t, body, "id")), "root@globex.localhost")
	globex := signIn(t, cfg, globexHost, "root@globex.localhost", adminPass)

	create := func(client *http.Client, host string) (int, string) {
		req, err := http.NewRequest(http.MethodPost, "http://"+cfg.Server.Addr+tasksPath, strings.NewReader(`{"title":"shared key"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Host = host
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(httpx.IdempotencyKeyHeader, "offline.task.shared")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		_, _ = io.Copy(io.Discard, res.Body)
		return res.StatusCode, res.Header.Get(httpx.OperationLocationHeader)
	}
	acmeStatus, acmeLocation := create(acme, acmeHost)
	globexStatus, globexLocation := create(globex, globexHost)
	if acmeStatus != http.StatusCreated || globexStatus != http.StatusCreated {
		t.Fatalf("creates = acme %d, globex %d; want 201 for both", acmeStatus, globexStatus)
	}
	if acmeLocation == "" || globexLocation == "" || acmeLocation == globexLocation {
		t.Fatalf("locations = %q and %q, want two distinct receipts", acmeLocation, globexLocation)
	}
	if code, body := do(t, cfg, globex, http.MethodGet, globexHost, globexLocation, ""); code != http.StatusOK {
		t.Errorf("globex reads its own receipt = %d %s, want 200", code, body)
	}
	if code, body := do(t, cfg, globex, http.MethodGet, globexHost, acmeLocation, ""); code != http.StatusNotFound {
		t.Errorf("globex reads acme's receipt = %d %s, want 404", code, body)
	}
	if code, body := do(t, cfg, acme, http.MethodGet, acmeHost, globexLocation, ""); code != http.StatusNotFound {
		t.Errorf("acme reads globex's receipt = %d %s, want 404", code, body)
	}
}
