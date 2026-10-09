package main

// The workspace's face, asked of the running reference composition.
//
// Three things are asserted here, and they are the three the brief names as
// acceptance: what a caller with no session reads, that an unknown host is refused
// rather than answered, and that the document carries no private data. The last is
// worth reading twice, because the answer is a projection built field by field: the
// case pins the exact key set, so a field added later is a decision somebody wrote
// down here and not a drift out of a struct literal.

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
)

const connectionPath = "/api/v1/app/connection"

// connectionKeys is every key the document may answer, and nothing else. The shape
// of the assertion is the one apps/platformkit/openapi_contract_test.go uses for the
// catalogue: keys present, not substrings present, because a substring test passes
// beside a body that grew.
var connectionKeys = []string{
	"accent", "accentRatio", "methods", "name", "recovery", "revision", "signIn", "theme",
}

func TestTheConnectionDocumentAnswersBeforeSignIn(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, appOptions(cfg, c, app.All))

	// Anonymous: no session, no credential, nothing to be refused for lacking.
	code, body := do(t, cfg, nil, http.MethodGet, acmeHost, connectionPath, "")
	if code != http.StatusOK {
		t.Fatalf("GET %s with no session = %d %s", connectionPath, code, body)
	}
	document := connectionBody(t, body)

	// Nothing has been configured at this tenant, and the name is still there: the
	// tenant's own name is the fallback, which is the difference between a first
	// screen with a word on it and a first screen with a hole in it.
	if document["name"] != "Acme Corporation" {
		t.Errorf("an unconfigured workspace is named %v, want the tenant's own name", document["name"])
	}
	if _, answered := document["logoUrl"]; answered {
		t.Errorf("a workspace with no logo answers logoUrl anyway (%v): the key is omitted rather than emptied, because an empty src is a request against the document's own address", document["logoUrl"])
	}
	if document["accent"] != "#2563eb" {
		t.Errorf("the accent is %v, want the colour the kit ships", document["accent"])
	}
	if document["theme"] != "system" {
		t.Errorf("the theme is %v, want system", document["theme"])
	}
	if document["revision"] != float64(0) {
		t.Errorf("revision is %v for a tenant that never saved its site", document["revision"])
	}
	// The door the installation mounted, verbatim, and the four methods.
	if signin, _ := document["signIn"].(map[string]any); signin["address"] != pinnedSignInAPI {
		t.Errorf("signIn is %v, want the door this composition mounted at %s", document["signIn"], pinnedSignInAPI)
	}
	methods, _ := document["methods"].(map[string]any)
	if methods["password"] != true {
		t.Errorf("methods = %v, and this composition mounted the password door", document["methods"])
	}
	if methods["passkey"] != false || methods["oidc"] != false || methods["saml"] != false {
		t.Errorf("methods = %v; nothing turned these on, so a shell would offer a door that refuses", document["methods"])
	}
	// configure() names no mail host, so a recovery mail could not leave this
	// installation and the document says so rather than drawing the control.
	if recovery, _ := document["recovery"].(map[string]any); recovery["available"] != false {
		t.Errorf("recovery = %v with no mail server configured", document["recovery"])
	}
	if got := responseHeader(t, cfg, nil, acmeHost, connectionPath, "Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q: /api/v1/app is a session surface and the kernel answers every response on one no-store — a shell caches this under the host, the tenant and revision, not in a shared cache", got)
	}

	// Now configure the face, and read the same door again: the tenant's own
	// answers, with the ratios the write-time rule compared quoted back.
	if code, body := do(t, cfg, signIn(t, cfg, acmeHost, adminEmail, adminPass), http.MethodPut, acmeHost,
		sitePath, `{"title":"Acme Works","tagline":"We make things","theme":"dark","primaryColor":"#b45309"}`); code != http.StatusOK {
		t.Fatalf("setting the site = %d %s", code, body)
	}
	code, body = do(t, cfg, nil, http.MethodGet, acmeHost, connectionPath, "")
	if code != http.StatusOK {
		t.Fatalf("GET %s after a save = %d %s", connectionPath, code, body)
	}
	document = connectionBody(t, body)
	if document["name"] != "Acme Works" {
		t.Errorf("the name is %v, want the title the tenant typed", document["name"])
	}
	if document["accent"] != "#b45309" || document["theme"] != "dark" {
		t.Errorf("the look reads back as %v / %v", document["accent"], document["theme"])
	}
	ratio, _ := document["accentRatio"].(map[string]any)
	if ratio["light"] != 4.37 || ratio["dark"] != 3.66 {
		t.Errorf("accentRatio = %v, want 4.37 and 3.66", ratio)
	}
	if document["revision"] != float64(1) {
		t.Errorf("revision is %v after one save: the row counts its own writes", document["revision"])
	}
	// The tagline a browser may already read is not in a document nothing reads
	// it from yet — rule 7, and the key set above is where that is enforced.
	if _, answered := document["tagline"]; answered {
		t.Errorf("the document answers tagline, which no consumer reads: %s", body)
	}
}

// TestAnUnknownHostIsRefusedAndNotAnsweredWithAnEmptyBody is the acceptance case the
// brief names, and the reason the 404 is written in the handler rather than left to
// a guard: a route declaring httpx.Public() is let through the tenant middleware
// with no tenant and no transaction, and a 200 whose body is empty would be an
// oracle for which hosts this installation serves.
func TestAnUnknownHostIsRefusedAndNotAnsweredWithAnEmptyBody(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, appOptions(cfg, c, app.All))

	code, body := do(t, cfg, nil, http.MethodGet, "nowhere.localhost", connectionPath, "")
	if code != http.StatusNotFound {
		t.Errorf("GET %s at a host nobody serves = %d %s, want 404", connectionPath, code, body)
	}
	if strings.Contains(body, "\"name\"") {
		t.Errorf("the refusal at an unknown host answers a workspace anyway: %s", body)
	}
	// The same door at that host, with somebody else's session: the answer is about
	// the host and not about the caller, so a credential changes nothing.
	if code, _ := do(t, cfg, signIn(t, cfg, acmeHost, adminEmail, adminPass), http.MethodGet, "nowhere.localhost", connectionPath, ""); code != http.StatusNotFound {
		t.Errorf("GET %s at an unknown host holding another tenant's session = %d, want 404", connectionPath, code)
	}
}

// TestTheConnectionDocumentCarriesNoPrivateData is the acceptance case that outlives
// this round: a field added to the projection has to be a key added here too, and
// the second half reads the bytes for what a workspace would never publish to an
// anonymous caller.
func TestTheConnectionDocumentCarriesNoPrivateData(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, appOptions(cfg, c, app.All))

	// A tenant with a person and a task, so that "no private data" is a claim about
	// a populated workspace and not about an empty one.
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	if code, body := do(t, cfg, admin, http.MethodPost, acmeHost, tasksPath, `{"title":"Ship the face"}`); code != http.StatusCreated {
		t.Fatalf("creating a task = %d %s; this case cannot be asked of a tenant with nothing in it", code, body)
	}

	code, body := do(t, cfg, nil, http.MethodGet, acmeHost, connectionPath, "")
	if code != http.StatusOK {
		t.Fatalf("GET %s = %d %s", connectionPath, code, body)
	}
	document := connectionBody(t, body)
	got := make([]string, 0, len(document))
	for key := range document {
		// $schema is the kernel's own member, not this projection's: the JSON
		// responder stamps a typed body with the dialect its schema is written in
		// and links the same document (kit/httpx/fault.go). It is why the case
		// below reads the body's keys and not its bytes.
		if key == "$schema" {
			continue
		}
		got = append(got, key)
	}
	slices.Sort(got)
	if !slices.Equal(got, connectionKeys) {
		t.Errorf("the document answers %v; the projection is exactly %v", got, connectionKeys)
	}

	for _, forbidden := range []string{
		adminEmail, "email", "userId", "user", "task", "total", "count", "plan",
		"slug", "tenant", "secret", "createdat", "updatedat", "homeSlug", "nav",
	} {
		if strings.Contains(strings.ToLower(strings.ReplaceAll(body, "Acme Corporation", "")), forbidden) {
			t.Errorf("the connection document contains %q: %s", forbidden, body)
		}
	}
}

// TestTheConnectionIsInTheContractADeviceIsGeneratedFrom keeps the device contract
// and the mount in the same commit's honest pair: an address in the document that
// nothing answers at is a redirect into a 404 with an application behind it.
func TestTheConnectionIsInTheContractADeviceIsGeneratedFrom(t *testing.T) {
	t.Parallel()
	doc := wireDocument(t, mustReadOpenAPIGolden(t))
	route, ok := wireRoute(doc, "app-connection")
	if !ok {
		t.Fatalf("%s publishes no app-connection operation", openapiGolden)
	}
	schema := wireResolveRef(doc, jsonResponseSchema(route))
	if schema == nil {
		t.Fatalf("app-connection has no application/json response schema in %s", openapiGolden)
	}
	for _, name := range connectionKeys {
		if _, ok := wireMap(schema["properties"])[name]; !ok {
			t.Errorf("the published connection document has no %q: the shape a shell parses is not in the contract", name)
		}
	}
	// logoUrl is the one key the answer may omit, and the document has to say so.
	if required := wireStringList(schema["required"]); slices.Contains(required, "logoUrl") {
		t.Errorf("the connection document requires logoUrl; a workspace with no mark answers no such key: %v", required)
	}
}

// TestOneHostsFaceIsNotAnotherHostsFace is the tenant-isolation case, and it is the
// only shape this door can be asked in: there is no tenant id in the path or the
// query, so a wrong-tenant read is not refused by a check here but by the absence
// of any way to ask — and by row-level security behind the transaction the host
// resolution opened. What the case can do is prove the two answers stay apart:
// Acme configured a face, Globex never did, and each host is told its own.
func TestOneHostsFaceIsNotAnotherHostsFace(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, appOptions(cfg, c, app.All))

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"globex","name":"Globex Corporation","host":"`+globexHost+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s; the case cannot be asked with one tenant", tenantPath, code, body)
	}
	globexID := field(t, body, "id")
	if code, body := do(t, cfg, admin, http.MethodPut, acmeHost, sitePath,
		`{"title":"Acme Works","theme":"dark","primaryColor":"#b45309"}`); code != http.StatusOK {
		t.Fatalf("setting Acme's site = %d %s", code, body)
	}

	acme := connectionBody(t, mustBe200(t, cfg, acmeHost))
	globex := connectionBody(t, mustBe200(t, cfg, globexHost))
	switch {
	case acme["name"] != "Acme Works" || acme["accent"] != "#b45309":
		t.Errorf("Acme's own host answers %v / %v", acme["name"], acme["accent"])
	case globex["name"] != "Globex Corporation":
		t.Errorf("Globex's host answers the name %v: it is being told about another tenant's site, or about none", globex["name"])
	case globex["accent"] != "#2563eb" || globex["theme"] != "system":
		t.Errorf("Globex's host answers Acme's look: %v / %v", globex["accent"], globex["theme"])
	}

	// And a suspended tenant answers this door the way it answers every door at its
	// own host: nothing is served here. That is a fact about the host, not a locked
	// door, and not an empty 200 either.
	if code, body := do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath+"/"+globexID+"/suspend", ""); code != http.StatusOK {
		t.Fatalf("suspend = %d %s", code, body)
	}
	if code, body = do(t, cfg, nil, http.MethodGet, globexHost, connectionPath, ""); code != http.StatusNotFound {
		t.Errorf("a suspended tenant's host answers %d %s, want 404", code, body)
	}
	if code, _ := do(t, cfg, nil, http.MethodGet, acmeHost, connectionPath, ""); code != http.StatusOK {
		t.Errorf("suspending a neighbour took Acme's face away too: %d", code)
	}
}

// mustBe200 is the connection document at one host, or the failure that says which
// host refused.
func mustBe200(t *testing.T, cfg config.Config, host string) string {
	t.Helper()
	code, body := do(t, cfg, nil, http.MethodGet, host, connectionPath, "")
	if code != http.StatusOK {
		t.Fatalf("GET %s at %s = %d %s", connectionPath, host, code, body)
	}
	return body
}

func connectionBody(t *testing.T, body string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("the connection document is not a JSON object: %v — %s", err, body)
	}
	return out
}

// responseHeader asks the running server for one header of one response. do gives
// the status and the body, which is enough for every other case here and not for a
// claim about caching.
func responseHeader(t *testing.T, cfg config.Config, client *http.Client, host, path, header string) string {
	t.Helper()
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequest(http.MethodGet, "http://"+cfg.Server.Addr+path, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Host = host
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s at %s: %v", path, host, err)
	}
	defer res.Body.Close()
	_, _ = io.ReadAll(res.Body)
	return res.Header.Get(header)
}
