package internal_test

// A key retiring itself, from the outside: the one credential command a scoped
// key may run, with no management grant, and only on the key it presented.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

const revokeSelf = "/api/v1/auth/tokens/revoke-self"

// mintKey issues a key scoped to role:manage through the session's own door and
// returns its token and id.
func mintKey(t *testing.T, router chi.Router, session, name string) (token, id string) {
	t.Helper()
	res := call(t, router, http.MethodPost, "/api/v1/auth/tokens",
		`{"name":"`+name+`","scopes":["role:manage"]}`, withSession(session))
	if res.Code != http.StatusCreated {
		t.Fatalf("issue %s = %d %s, want 201", name, res.Code, res.Body.String())
	}
	var issued struct{ Token, ID string }
	if err := json.Unmarshal(res.Body.Bytes(), &issued); err != nil {
		t.Fatalf("read the issue response: %v (%s)", err, res.Body.String())
	}
	return issued.Token, issued.ID
}

func refusedWith(t *testing.T, res *httptest.ResponseRecorder, code, what string) {
	t.Helper()
	if res.Code != http.StatusForbidden || !strings.Contains(res.Body.String(), code) {
		t.Errorf("%s = %d %s, want 403 %s", what, res.Code, res.Body.String(), code)
	}
}

// challenged asserts the kernel's 401 for a request that brought no key it
// recognises to a key-only door, with the Bearer challenge that names the scheme.
func challenged(t *testing.T, res *httptest.ResponseRecorder, challenge, what string) {
	t.Helper()
	if res.Code != http.StatusUnauthorized || !strings.Contains(res.Body.String(), httpx.CodeAnonymous) {
		t.Errorf("%s = %d %s, want 401 %s", what, res.Code, res.Body.String(), httpx.CodeAnonymous)
	}
	if got := res.Header().Get("WWW-Authenticate"); got != challenge {
		t.Errorf("%s challenged with %q, want %q", what, got, challenge)
	}
}

// TestAKeyMayRevokeItselfWithoutAnyManagementGrant: the key holds role:manage
// and nothing about keys — there is no token-management permission to hold.
func TestAKeyMayRevokeItselfWithoutAnyManagementGrant(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	ada := signIn(t, router, "ada@acme.localhost")
	key, _ := mintKey(t, router, ada, "Phone")

	res := call(t, router, http.MethodPost, revokeSelf, "", bearer(key))
	if res.Code != http.StatusNoContent || res.Body.Len() != 0 {
		t.Fatalf("a key revoking itself = %d %q, want 204 with no body", res.Code, res.Body.String())
	}
	if after := call(t, router, http.MethodGet, "/api/v1/auth/roles", "", bearer(key)); after.Code != http.StatusForbidden {
		t.Errorf("the retired key at the roles route = %d, want 403", after.Code)
	}
	if still := call(t, router, http.MethodGet, "/api/v1/auth/roles", "", withSession(ada)); still.Code != http.StatusOK {
		t.Errorf("a key retiring itself cost its holder their session: %d", still.Code)
	}
	list := call(t, router, http.MethodGet, "/api/v1/auth/tokens", "", withSession(ada))
	if !strings.Contains(list.Body.String(), `"revokedAt"`) {
		t.Errorf("the holder's list does not show the key revoked: %s", list.Body.String())
	}
}

func TestAKeyRetiresItselfAndLeavesItsHoldersOtherKeyStanding(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	ada := signIn(t, router, "ada@acme.localhost")
	phone, _ := mintKey(t, router, ada, "Phone")
	laptop, _ := mintKey(t, router, ada, "Laptop")

	if res := call(t, router, http.MethodPost, revokeSelf, "", bearer(phone)); res.Code != http.StatusNoContent {
		t.Fatalf("the phone's key revoking itself = %d %s, want 204", res.Code, res.Body.String())
	}
	if res := call(t, router, http.MethodGet, "/api/v1/auth/roles", "", bearer(laptop)); res.Code != http.StatusOK {
		t.Errorf("the holder's other key at the roles route = %d, want 200", res.Code)
	}
	if res := call(t, router, http.MethodGet, "/api/v1/auth/roles", "", bearer(phone)); res.Code != http.StatusForbidden {
		t.Errorf("the retired key at the roles route = %d, want 403", res.Code)
	}
}

// TestASessionIsRefusedTheSelfRevokeAndLosesNoKey: the kernel refuses a session
// at a key-only door with 403, not the CSRF refusal, and no key is touched.
func TestASessionIsRefusedTheSelfRevokeAndLosesNoKey(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	ada := signIn(t, router, "ada@acme.localhost")
	key, _ := mintKey(t, router, ada, "Phone")

	refusedWith(t, call(t, router, http.MethodPost, revokeSelf, "", withSession(ada)),
		"a session carries none", "a session at the self-revoke")
	if res := call(t, router, http.MethodGet, "/api/v1/auth/roles", "", bearer(key)); res.Code != http.StatusOK {
		t.Errorf("a refused self-revoke ended the holder's key: %d", res.Code)
	}
	list := call(t, router, http.MethodGet, "/api/v1/auth/tokens", "", withSession(ada))
	if strings.Contains(list.Body.String(), `"revokedAt"`) {
		t.Errorf("a refused self-revoke revoked a key: %s", list.Body.String())
	}
}

// TestADeadKeyNeverReachesTheSelfRevoke: the kernel's 401 challenge, not the
// handler's refusal, is the proof the request stopped before the command.
func TestADeadKeyNeverReachesTheSelfRevoke(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	ada := signIn(t, router, "ada@acme.localhost")
	key, id := mintKey(t, router, ada, "Phone")
	if res := call(t, router, http.MethodPost, "/api/v1/auth/tokens/"+id+"/revoke", "", withSession(ada)); res.Code != http.StatusOK {
		t.Fatalf("revoke by id = %d %s", res.Code, res.Body.String())
	}
	challenged(t, call(t, router, http.MethodPost, revokeSelf, "", bearer(key)),
		`Bearer error="invalid_token"`, "a revoked key at the self-revoke")
}

// TestTheSelfRevokeIsTheOneCredentialDoorAKeyMayUse re-pins the ban beside its
// exception: the same key is refused at every other token door.
func TestTheSelfRevokeIsTheOneCredentialDoorAKeyMayUse(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	ada := signIn(t, router, "ada@acme.localhost")
	key, id := mintKey(t, router, ada, "Phone")

	refusedWith(t, call(t, router, http.MethodPost, "/api/v1/auth/tokens", `{"name":"More","scopes":["role:manage"]}`, bearer(key)),
		httpx.CodeDenied, "a key minting a key")
	refusedWith(t, call(t, router, http.MethodGet, "/api/v1/auth/tokens", "", bearer(key)),
		httpx.CodeDenied, "a key listing keys")
	refusedWith(t, call(t, router, http.MethodPost, "/api/v1/auth/tokens/"+id+"/revoke", "", bearer(key)),
		httpx.CodeDenied, "a key revoking by id")
	challenged(t, call(t, router, http.MethodPost, revokeSelf, ""), "Bearer", "nobody at the self-revoke")
}

// TestAnotherTenantsKeyIsNotACredentialAtTheSelfRevoke: acme's key at globex's
// host is nobody, so it never reaches the command, and still works at acme.
func TestAnotherTenantsKeyIsNotACredentialAtTheSelfRevoke(t *testing.T) {
	router, conn := mountTwo(t, mapProviders{}, mapSecrets{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	key, _ := mintKey(t, router, signIn(t, router, "ada@acme.localhost"), "Phone")

	atGlobex := func(r *http.Request) { r.Host, r.URL.Host = secondHost, secondHost }
	challenged(t, call(t, router, http.MethodPost, revokeSelf, "", bearer(key), atGlobex),
		`Bearer error="invalid_token"`, "acme's key at globex's self-revoke")
	if res := call(t, router, http.MethodGet, "/api/v1/auth/roles", "", bearer(key)); res.Code != http.StatusOK {
		t.Errorf("acme's key at acme after globex refused it = %d, want 200", res.Code)
	}
}
