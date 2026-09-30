package internal_test

// A bearer key, arrived at from the outside: what it opens, what it refuses, and
// what it never appears in.
//
// The brief's item 4 is a credential for a program, so the cases are HTTP ones —
// a header on a request, not a call into the service — because the thing at issue
// is whether the platform recognises a caller who cannot hold a cookie. Five
// claims, in the order an incident would meet them:
//
//   - a key signed in by a person who holds role:manage opens the roles route;
//   - the same key never appears in the list of keys, which is a screen;
//   - a scope the holder does not have is refused at issue, and issues nothing;
//   - revoking the key stops it working, and the person's own sessions survive it;
//   - a request carrying both a cookie and a key is nobody, because which one the
//     caller meant is not the kernel's to guess.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

func TestABearerKeyOpensWhatItsHolderGrantedItAndNothingElse(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	person(t, conn, "bhavna@acme.localhost", contracts.RoleMember)
	ada := signIn(t, router, "ada@acme.localhost")

	issue := func(cookie, body string) *httptest.ResponseRecorder {
		t.Helper()
		return call(t, router, http.MethodPost, "/api/v1/auth/tokens", body, withSession(cookie))
	}

	res := issue(ada, `{"name":"Deploy bot","scopes":["role:manage"]}`)
	if res.Code != http.StatusCreated {
		t.Fatalf("issuing a key with a scope its holder has = %d %s, want 201", res.Code, res.Body.String())
	}
	var issued struct {
		Token string `json:"token"`
		ID    string `json:"id"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &issued); err != nil {
		t.Fatalf("read the issue response: %v (%s)", err, res.Body.String())
	}
	if !strings.HasPrefix(issued.Token, "pkit_") || len(issued.Token) < 40 {
		t.Fatalf("the issued token is %q: it must be a long secret with the prefix that says what it is", issued.Token)
	}

	// The key opens the route its one scope names.
	roles := call(t, router, http.MethodGet, "/api/v1/auth/roles", "", bearer(issued.Token))
	if roles.Code != http.StatusOK {
		t.Fatalf("a key scoped to role:manage at the roles route = %d %s, want 200", roles.Code, roles.Body.String())
	}

	// The list is a screen, not a bag of credentials: no key, no hash, no prefix.
	list := call(t, router, http.MethodGet, "/api/v1/auth/tokens", "", withSession(ada))
	if list.Code != http.StatusOK {
		t.Fatalf("listing keys = %d %s, want 200", list.Code, list.Body.String())
	}
	if strings.Contains(list.Body.String(), issued.Token) || strings.Contains(list.Body.String(), "pkit_") {
		t.Errorf("the list of keys carries a credential: %s", list.Body.String())
	}
	if !strings.Contains(list.Body.String(), "Deploy bot") {
		t.Errorf("the list does not carry the key's name: %s", list.Body.String())
	}

	// A scope the holder does not have, and a scope nothing defines: both refused,
	// and neither leaves a row — the second list still holds exactly one key.
	for _, body := range []string{
		`{"name":"Overreaching","scopes":["billing:manage"]}`,
		`{"name":"Undefined","scopes":["no.such:permission"]}`,
		`{"name":"Everything","scopes":["*"]}`,
		`{"name":"No scope","scopes":[]}`,
	} {
		bad := issue(ada, body)
		if bad.Code != http.StatusUnprocessableEntity {
			t.Errorf("issuing %s = %d %s, want 422", body, bad.Code, bad.Body.String())
		}
	}
	after := call(t, router, http.MethodGet, "/api/v1/auth/tokens", "", withSession(ada))
	if got := keys(t, after.Body.String()); got != 1 {
		t.Errorf("a refused issue left %d keys, want the 1 that worked", got)
	}
	// Bhavna holds no permission at all, so she can delegate nothing — not even a
	// scope that exists and is well-known.
	asMember := signIn(t, router, "bhavna@acme.localhost")
	if narrow := issue(asMember, `{"name":"Nothing to delegate","scopes":["role:manage"]}`); narrow.Code != http.StatusUnprocessableEntity {
		t.Errorf("a holder with nothing to delegate issued a key: %d %s", narrow.Code, narrow.Body.String())
	}

	// Revocation. The key stops, and the person's own session does not: a stolen
	// key and a stolen laptop are different incidents.
	revoke := call(t, router, http.MethodPost, "/api/v1/auth/tokens/"+issued.ID+"/revoke", "", withSession(ada))
	if revoke.Code != http.StatusOK {
		t.Fatalf("revoking = %d %s, want 200", revoke.Code, revoke.Body.String())
	}
	if stopped := call(t, router, http.MethodGet, "/api/v1/auth/roles", "", bearer(issued.Token)); stopped.Code != http.StatusForbidden {
		t.Errorf("a revoked key at the roles route = %d %s, want 403", stopped.Code, stopped.Body.String())
	}
	if still := call(t, router, http.MethodGet, "/api/v1/auth/roles", "", withSession(ada)); still.Code != http.StatusOK {
		t.Errorf("revoking a key cost the person their own session: %d", still.Code)
	}
	// An id that is not the caller's is a 404 and not a 403: which keys exist
	// elsewhere is not something a screen finds out by asking.
	if notMine := call(t, router, http.MethodPost, "/api/v1/auth/tokens/"+issued.ID+"/revoke", "", withSession(asMember)); notMine.Code != http.StatusNotFound {
		t.Errorf("revoking somebody else's key = %d, want 404", notMine.Code)
	}
}

// TestARequestCarryingBothCredentialsIsNobody is the refusal a client with a bug
// deserves, and the one an authorizer must never have to think about.
func TestARequestCarryingBothCredentialsIsNobody(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	ada := signIn(t, router, "ada@acme.localhost")
	res := call(t, router, http.MethodPost, "/api/v1/auth/tokens",
		`{"name":"Both","scopes":["role:manage"]}`, withSession(ada))
	var issued struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &issued); err != nil {
		t.Fatalf("read the issue response: %v", err)
	}
	both := call(t, router, http.MethodGet, "/api/v1/auth/roles", "", withSession(ada), bearer(issued.Token))
	if both.Code != http.StatusForbidden {
		t.Fatalf("a request carrying a cookie and a key = %d %s, want 403 as the anonymous request it is",
			both.Code, both.Body.String())
	}
	// The same cookie on its own is still the person, so the refusal cost the
	// ambiguous request its credential and nothing more.
	if alone := call(t, router, http.MethodGet, "/api/v1/auth/roles", "", withSession(ada)); alone.Code != http.StatusOK {
		t.Errorf("the cookie alone stopped working after an ambiguous request: %d", alone.Code)
	}
}

// TestTheBearerShapeIsTheHeaderS says what the kernel accepts, without a database:
// the scheme is case-insensitive, the credential is what follows one space of it,
// and a request that names the scheme twice is refused rather than resolved.
func TestTheBearerShapeIsTheHeadersOwn(t *testing.T) {
	for _, table := range []struct {
		name    string
		headers []string
		want    string
	}{
		{"one credential", []string{"Bearer pkit_abc"}, "pkit_abc"},
		{"the scheme in another case", []string{"bearer pkit_abc"}, "pkit_abc"},
		{"trailing space", []string{"Bearer  pkit_abc"}, "pkit_abc"},
		{"another scheme is not ours", []string{"Basic pkit_abc"}, ""},
		{"the scheme with nothing after it", []string{"Bearer "}, ""},
		{"the same credential twice", []string{"Bearer pkit_abc", "Bearer pkit_abc"}, "pkit_abc"},
		{"two different credentials", []string{"Bearer pkit_abc", "Bearer pkit_def"}, ""},
	} {
		r := httptest.NewRequest(http.MethodGet, "http://"+host+"/", nil)
		for _, h := range table.headers {
			r.Header.Add("Authorization", h)
		}
		got, ok := httpx.BearerOf(r)
		if got != table.want || ok != (table.want != "") {
			t.Errorf("%s: BearerOf = (%q, %v), want (%q, %v)", table.name, got, ok, table.want, table.want != "")
		}
	}
}

// bearer attaches a bearer credential — the header a browser cannot set from a
// form, which is the whole of why this credential needs no CSRF rule of its own.
func bearer(token string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
}

func keys(t *testing.T, body string) int {
	t.Helper()
	var out struct {
		Total int `json:"total"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("read the key list: %v (%s)", err, body)
	}
	return out.Total
}

// The bearer key's scope is rechecked against the roles on every request, so
// standing a person down from a role narrows the keys they minted in the same
// transaction that did it. This is that claim, and it fails if the intersection
// is cached, stored on the row, or skipped.
func TestStandingARoleDownNarrowsTheKeysThatPersonMinted(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	id := person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	person(t, conn, "bhavna@acme.localhost", contracts.RoleAdmin)
	res := call(t, router, http.MethodPost, "/api/v1/auth/tokens",
		`{"name":"Deploy bot","scopes":["role:manage"]}`, withSession(signIn(t, router, "ada@acme.localhost")))
	var issued struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &issued); err != nil {
		t.Fatalf("read the issue response: %v (%s)", err, res.Body.String())
	}
	if open := call(t, router, http.MethodGet, "/api/v1/auth/roles", "", bearer(issued.Token)); open.Code != http.StatusOK {
		t.Fatalf("the key at the roles route = %d, want 200 before anything changed", open.Code)
	}
	// The person is now a member, which grants nothing.
	users := realUsers()
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := users.SetRoles(ctx, tx, id, usercontracts.Roles{contracts.RoleMember})
		return err
	}); err != nil {
		t.Fatalf("stand ada down: %v", err)
	}
	if closed := call(t, router, http.MethodGet, "/api/v1/auth/roles", "", bearer(issued.Token)); closed.Code != http.StatusForbidden {
		t.Errorf("a key whose holder has been stood down still opens the roles route: %d %s — the scopes are read as stored rather than recomputed",
			closed.Code, closed.Body.String())
	}
}
