package main

// The reference application's own side of the SCIM door.
//
// The module's cases ask what the service decides; the httpx cases ask what the
// kernel allows an address to be. Neither answers the question a tenant's identity
// provider actually poses: does this installation answer at the address the
// protocol names, with the credential I was told to use, and is the person I
// provisioned the same person the tenant's own screen lists. That is a question
// about the composition, so it is asked of the composition: one running reference
// application, its tenant's host, and four callers.

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

// scimDoor is the address the module declares — written out rather than derived,
// because the whole claim is that a client configures exactly this.
const scimDoor = "/scim/v2/Users"

// scimAsk sends one request at the running application with the content type and
// the credential the protocol's client would send. do always says
// application/json and never a bearer token, which are the two facts this case is
// about.
func scimAsk(t *testing.T, cfg config.Config, method, host, path, body, token, contentType string) (int, http.Header, string) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, "http://"+cfg.Server.Addr+path, reader)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Host = host
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s at %s: %v", method, path, host, err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, string(out)
}

// scimKey mints a credential scoped to one permission, as the tenant's own
// administrator would: the token door is auth's and the scopes are the ceiling.
func scimKey(t *testing.T, cfg config.Config, admin *http.Client, scopes ...string) string {
	t.Helper()
	list := make([]string, 0, len(scopes))
	list = append(list, scopes...)
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, "/api/v1/auth/tokens",
		`{"name":"SCIM connector","scopes":`+jsonStringList(list)+`}`)
	if code != http.StatusCreated {
		t.Fatalf("minting a key scoped to %v = %d %s, want 201", scopes, code, body)
	}
	return field(t, body, "token")
}

func jsonStringList(in []string) string {
	out := "["
	for i, s := range in {
		if i > 0 {
			out += ","
		}
		out += `"` + s + `"`
	}
	return out + "]"
}

// TestTheSCIMDoorAnswersAtTheAddressTheProtocolNames walks four callers through
// one address of the running reference application.
func TestTheSCIMDoorAnswersAtTheAddressTheProtocolNames(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	// 1. Nobody. The workspace's refusal, at a protocol's own address: the
	// declaration moved where the door stands, and a door that answered an
	// anonymous caller because it stood outside /api/v1 would be the hole the
	// mount gate exists to refuse.
	code, _, body := scimAsk(t, cfg, http.MethodGet, acmeHost, scimDoor, "", "", "")
	if code == http.StatusOK || code == http.StatusNotFound {
		t.Fatalf("an anonymous GET of the SCIM collection = %d %s; the door is neither open to nobody nor missing — it is the workspace's, and the workspace refuses", code, body)
	}
	if !strings.Contains(body, "AUTH_ANONYMOUS") {
		t.Errorf("the anonymous refusal is not the kernel's own verdict: %s", body)
	}

	// 2. A key whose scope names a different door entirely.
	narrow := scimKey(t, cfg, admin, "task:read")
	if code, _, body = scimAsk(t, cfg, http.MethodGet, acmeHost, scimDoor, "", narrow, ""); code == http.StatusOK {
		t.Fatalf("a key scoped to task:read was served the SCIM directory: %s", body)
	}

	// 3. A key with the reach, in the protocol's own media type, and the answer
	// comes back named the same way.
	key := scimKey(t, cfg, admin, "scim:provision")
	code, header, body := scimAsk(t, cfg, http.MethodGet, acmeHost, scimDoor, "", key, "application/scim+json")
	if code != http.StatusOK {
		t.Fatalf("a key scoped to scim:provision = %d %s, want 200 — this is the door the permission is for", code, body)
	}
	if ct := header.Get("Content-Type"); !strings.HasPrefix(ct, "application/scim+json") {
		t.Errorf("the list came back as %q, want the media type the protocol names", ct)
	}
	var list struct {
		Schemas      []string `json:"schemas"`
		TotalResults int      `json:"totalResults"`
	}
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatalf("the collection is not the protocol's list document: %s", body)
	}
	if len(list.Schemas) == 0 || !strings.Contains(list.Schemas[0], "ListResponse") {
		t.Errorf("the list names schemas %v, want the ListResponse urn", list.Schemas)
	}

	// 4. A person, provisioned in the media type the connector sends. A request
	// whose Content-Type nobody registered is a 415 ahead of every route, so this
	// line is the composition's promise and not the module's.
	code, header, body = scimAsk(t, cfg, http.MethodPost, acmeHost, scimDoor,
		`{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"connector@acme.test","displayName":"Connector Person","active":true}`,
		key, "application/scim+json")
	if code != http.StatusCreated {
		t.Fatalf("provisioning through the SCIM door = %d %s, want 201", code, body)
	}
	if ct := header.Get("Content-Type"); !strings.HasPrefix(ct, "application/scim+json") {
		t.Errorf("the created person came back as %q", ct)
	}
	provisioned := field(t, body, "id")
	if got := field(t, body, "userName"); got != "connector@acme.test" {
		t.Errorf("the person reads back userName %q", got)
	}

	// The same person, at the tenant's own door, on the tenant's own screen: one
	// row, one identity, written through modules/user's own commands. If SCIM had
	// grown a table of its own people, this is the line that would notice.
	code, body = do(t, cfg, admin, http.MethodGet, acmeHost,
		"/api/v1/user/users/"+provisioned, "")
	if code != http.StatusOK {
		t.Fatalf("reading the person back at the workspace door = %d %s", code, body)
	}
	if !strings.Contains(body, "connector@acme.test") {
		t.Errorf("the workspace does not list the person SCIM created: %s", body)
	}

	// 5. A host that resolves no tenant is answered the way an address nobody
	// mounted is, and not with a different-shaped door for the protocol.
	if code, _, body = scimAsk(t, cfg, http.MethodGet, "nowhere.invalid", scimDoor, "", key, ""); code == http.StatusOK {
		t.Fatalf("the SCIM door answered a host that resolves no tenant: %s", body)
	}
}
