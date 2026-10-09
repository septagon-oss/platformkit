package main

// The header names the person who is signed in by the name they chose, else by
// the address they sign in with — read through the composed user module in the
// request's own tenant transaction, not through a fake.

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

func TestTheHeaderNamesTheSignedInPersonNotTheirID(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, app.Options{
		Tenants: c.tenants, Authorize: c.auth, Entitle: c.plans, Authenticate: c.auth.Authenticate,
		Role: app.All, Transport: memory.New(), Log: quiet(),
	})
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	var me map[string]any
	if err := json.Unmarshal([]byte(whoami(t, cfg, admin)), &me); err != nil {
		t.Fatal(err)
	}
	id, _ := me["id"].(string)
	if id == "" {
		id, _ = me["userId"].(string)
	}
	if len(id) < 8 {
		t.Fatalf("GET /api/v1/auth/me names no id: %v", me)
	}
	name, _ := me["displayName"].(string)
	name = strings.TrimSpace(name)
	want := adminEmail
	if name != "" {
		want = name
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+cfg.Server.Addr+"/app", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = acmeHost
	req.Header.Set("Accept", "text/html")
	res, err := admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	page := string(body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /app signed in = %d, want 200:\n%.400s", res.StatusCode, page)
	}
	if !strings.Contains(page, want) {
		t.Errorf("the signed-in page does not name the person as %q", want)
	}
	if strings.Contains(page, id[:8]+" ·") {
		t.Errorf("the signed-in page still names the person by an id fragment %q", id[:8])
	}
}

// TestTheHeaderNamesNobodyFromAnotherTenant is the same line read across two
// tenants: each administrator is named by their own address at their own host, and
// a session carried to the other tenant's host names nobody at all.
func TestTheHeaderNamesNobodyFromAnotherTenant(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, app.Options{
		Tenants: c.tenants, Authorize: c.auth, Entitle: c.plans, Authenticate: c.auth.Authenticate,
		Role: app.All, Transport: memory.New(), Log: quiet(),
	})
	acme := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	code, body := do(t, cfg, acme, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"globex","name":"Globex","host":"`+globexHost+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", tenantPath, code, body)
	}
	provision(t, cfg, uuid.MustParse(field(t, body, "id")), "root@globex.localhost")
	globex := signIn(t, cfg, globexHost, "root@globex.localhost", adminPass)

	page := func(client *http.Client, host string) (int, string) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+cfg.Server.Addr+"/app", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = host
		req.Header.Set("Accept", "text/html")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		out, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(out)
	}
	if code, got := page(globex, globexHost); code != http.StatusOK || !strings.Contains(got, "root@globex.localhost") || strings.Contains(got, adminEmail) {
		t.Errorf("globex's own page = %d; want 200 naming root@globex.localhost and never %s", code, adminEmail)
	}
	if _, got := page(acme, globexHost); strings.Contains(got, adminEmail) || strings.Contains(got, "root@globex.localhost") {
		t.Errorf("acme's session at globex's host names a person; it should name nobody")
	}
}
