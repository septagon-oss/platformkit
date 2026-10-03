package main

// A key that opens no door is not a credential, asked of the cure that widened the refusal.
//
// The fix holds a credential that carries its own ceiling to that ceiling by
// refusing it every operation that names no permission — that is, every
// `httpx.SignedIn()` door in the application:
//
// 	if auth.kind == kindSignedIn && p.Permissions != nil { deny }
//
// The refusal is right for the self-service doors it first named. This case asks
// what else that sentence covers, because the same kernel mounts one further
// SignedIn operation that is nobody's self-service door:
//
// 	kernel := api.Surfaces("")
// 	httpx.Register(kernel.App, huma.Operation{OperationID: "app-resources", …},
// 		httpx.SignedIn(), …)                                   // kit/app/app.go
//
// `GET /api/v1/app/resources` is the document a shell that is not a browser
// builds its whole vocabulary out of — the composition gate refuses a
// composition that registers resources and wires no renderer for exactly that
// reason ("a native shell has nothing to read — wire app.Options
//
// 	.WorkspaceCatalog"), and decision 0019 makes it the mobile counterpart's
// 	first request. And the credential a native shell holds is the bearer key:
// 	brief item 4, "Bearer tokens for the mobile shell", and this module's own
// 	summary of why the table exists at all — "the only credential this platform
// 	accepted was a cookie, so a mobile shell or a deploy bot had to hold a
// 	person's password and impersonate their browser".
//
// So the fix left the installation with a credential that may call every
// permission-named route and no door that tells it what the routes are: the key
// is minted by a browser, and after that it cannot read the catalogue. The
// module README's *Open here* names exactly one thing a bearer caller is refused
// — `GET /api/v1/auth/me` — and this is a second, larger one.
//
// The assertion is about the catalogue, and it is reached only through what the
// correct behaviour prints: a 200 whose document names the tasks resource by
// path and by permission. Nothing here depends on the refusal's wording, its
// code, or the existence of the refusal; and the same request through the
// person's own session is a 200 both today and after any fix, so the case cannot
// be satisfied by refusing the catalog to everybody.

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

const catalogPath = "/api/v1/app/resources"

// TestAScopedKeyReadsTheCatalogAShellIsBuiltFrom mints one ordinary key with a
// session cookie — the only way a key is ever minted — and asks it for the
// document every client that is not a browser starts from.
func TestAScopedKeyReadsTheCatalogAShellIsBuiltFrom(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, "/api/v1/auth/tokens",
		`{"name":"The mobile shell","scopes":["task:read"]}`)
	if code != http.StatusCreated {
		t.Fatalf("minting a key scoped to task:read = %d %s, want 201", code, body)
	}
	key := field(t, body, "token")

	// The key is a working credential at the door its scope names. Reachability,
	// and it holds today and after any fix.
	if code, got := doKey(t, cfg, key, http.MethodGet, acmeHost, tasksPath, ""); code != http.StatusOK {
		t.Fatalf("a key scoped to task:read at %s = %d %s, want 200: this case is about a credential "+
			"that can already do real work", tasksPath, code, got)
	}

	// The same document, read the person's own way: 200 with a session, which is
	// the half the fix must not break either.
	if code, _ := do(t, cfg, admin, http.MethodGet, acmeHost, catalogPath, ""); code != http.StatusOK {
		t.Fatalf("the catalog through the session that minted the key = %d, want 200", code)
	}

	code, body = doKey(t, cfg, key, http.MethodGet, acmeHost, catalogPath, "")
	if code != http.StatusOK {
		t.Errorf("the key a person minted for their mobile shell reading %s = %d %s, want 200 — "+
			"kit/app mounts that route httpx.SignedIn(), and 61e5f34 refuses every SignedIn operation "+
			"to a credential carrying its own scopes, so the bearer token the brief's item 4 minted for "+
			"the mobile shell cannot read the document a shell that is not a browser is built from "+
			"(decision 0019). The kernel's own composition gate refuses a build where \"a native shell "+
			"has nothing to read\"; this is that sentence arrived at by another route.", catalogPath, code, trimHTML(body))
		return
	}
	var doc struct {
		Resources []struct {
			Module string `json:"module"`
			Path   string `json:"path"`
		} `json:"resources"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("the catalog is not the document a shell parses: %v\n%s", err, body)
	}
	var named bool
	var beyondScope []string
	for _, r := range doc.Resources {
		if r.Path == tasksPath {
			named = true
		}
		if r.Module != "task" {
			beyondScope = append(beyondScope, r.Module+" "+r.Path)
		}
	}
	if !named {
		t.Errorf("the catalog the key read names no %s among its %d resources: %s", tasksPath, len(doc.Resources), body)
	}
	// The reason the refusal is worth arguing about: this document is already
	// narrowed by the credential's own scopes — screens.Describe asks Readable,
	// Writable and CommandsFor, which go through the authorizer, which for a
	// scoped caller consults the scopes and not the roles. So the door the kernel
	// now shuts is one that was already shut on the far side of it: a key scoped
	// to one permission reads the one resource that permission opens, and a
	// person's own session reads five.
	if len(beyondScope) > 0 {
		t.Errorf("a key scoped to task:read was shown %d resource(s) outside that scope (%v): the catalog "+
			"is not filtered by the credential, and the refusal in this case is then doing work no authorizer "+
			"does", len(beyondScope), beyondScope)
	}
	_, asSession := do(t, cfg, admin, http.MethodGet, acmeHost, catalogPath, "")
	var wider struct {
		Resources []json.RawMessage `json:"resources"`
	}
	if err := json.Unmarshal([]byte(asSession), &wider); err != nil {
		t.Fatalf("the same document through the session: %v\n%s", err, asSession)
	}
	if len(wider.Resources) <= len(doc.Resources) {
		t.Errorf("the session that minted the key was shown %d resources and the key %d, want the session "+
			"to see more: this case asks for a narrowed document, not for the same document twice", len(wider.Resources), len(doc.Resources))
	}
}

// TestAKeyRequestMintsNoSessionAnywhere is the README sentence the fix left true,
// pinned because nothing else reads it: "A token request never sets or rotates a
// session cookie", which is kit/httpx authenticate.go's own promise too ("No
// cookie is ever set, rotated or cleared by a request that arrived on a bearer").
// It holds after 61e5f34 whatever one thinks of the refusals the fix introduced,
// so it is the assertion a later cure must not break. What it deliberately does
// not assert is the *next* clause of that README sentence — "POST /logout still
// answers a cleared cookie" — because logout is httpx.SignedIn() and that request
// is now a 403, and a cleared cookie is not a
// session either way.
func TestAKeyRequestMintsNoSessionAnywhere(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, "/api/v1/auth/tokens",
		`{"name":"Read only","scopes":["task:read"]}`)
	if code != http.StatusCreated {
		t.Fatalf("minting the key = %d %s", code, body)
	}
	key := field(t, body, "token")

	// Reachability through what correct behaviour prints: the key does real work.
	if code, _ := doKey(t, cfg, key, http.MethodGet, acmeHost, tasksPath, ""); code != http.StatusOK {
		t.Fatalf("the key at %s = %d, want 200 before the doors a key request is about", tasksPath, code)
	}

	for _, door := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/auth/logout"},
		{http.MethodPost, "/api/v1/auth/tokens"},
		{http.MethodGet, catalogPath},
		{http.MethodGet, "/api/v1/auth/me"},
		{http.MethodGet, tasksPath},
	} {
		if live := liveSessionCookiesOfKeyRequest(t, cfg, key, door.method, door.path); len(live) != 0 {
			t.Errorf("a bearer request %s %s set a cookie carrying a value (%v), want none — a key is not a "+
				"session, and a response that remembers a bearer caller is a response that minted one",
				door.method, door.path, live)
		}
	}
}

// liveSessionCookiesOfKeyRequest sends one request on a key alone — no cookie
// jar, no Cookie header — and returns every Set-Cookie that carries a value,
// which is the only kind that opens or rotates a session. A cleared cookie is not
// a session, so it is not counted, and the case cannot be turned into an argument
// about whether a key may log itself out.
func liveSessionCookiesOfKeyRequest(t *testing.T, cfg config.Config, key, method, path string) []string {
	t.Helper()
	req, err := http.NewRequest(method, "http://"+cfg.Server.Addr+path, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Host = acmeHost
	req.Header.Set("Authorization", "Bearer "+key)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	io.Copy(io.Discard, res.Body)
	var live []string
	for _, raw := range res.Header.Values("Set-Cookie") {
		_, value, _ := strings.Cut(raw, "=")
		if before, _, found := strings.Cut(value, ";"); found {
			value = before
		}
		if value != "" {
			live = append(live, raw)
		}
	}
	return live
}

// TestAScopedKeyStillRefusesTheDoorsItWasNeverGiven is the other half, so the fix
// above cannot be bought by admitting a scoped key everywhere: the self-service
// doors stay shut, and the roles route stays shut to a key scoped
// to a read.
func TestAScopedKeyStillRefusesTheDoorsItWasNeverGiven(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, "/api/v1/auth/tokens",
		`{"name":"Read only","scopes":["task:read"]}`)
	if code != http.StatusCreated {
		t.Fatalf("minting the key = %d %s", code, body)
	}
	key := field(t, body, "token")

	for _, door := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/auth/tokens", `{"name":"Wider","scopes":["role:manage"]}`},
		{http.MethodPost, "/api/v1/auth/sessions/revoke-all", ``},
		{http.MethodPost, "/api/v1/auth/factors/recovery/rotate", ``},
		{http.MethodGet, "/api/v1/auth/sessions", ``},
		{http.MethodPut, "/api/v1/auth/roles/admin", `{"permissions":["*"]}`},
	} {
		code, got := doKey(t, cfg, key, door.method, acmeHost, door.path, door.body)
		if code < http.StatusBadRequest {
			t.Errorf("a key scoped to task:read at %s %s = %d %s, want a refusal",
				door.method, door.path, code, got)
		}
	}
}

// doKey is do with a bearer credential and no cookie: what a shell that is not a
// browser presents.
func doKey(t *testing.T, cfg config.Config, key, method, host, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, "http://"+cfg.Server.Addr+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Host = host
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s at %s: %v", method, path, host, err)
	}
	defer res.Body.Close()
	out := make([]byte, 0, 4096)
	buf := make([]byte, 4096)
	for {
		n, err := res.Body.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			break
		}
	}
	return res.StatusCode, string(out)
}
