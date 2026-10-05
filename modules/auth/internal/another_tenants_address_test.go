package internal_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
)

// TestAnAddressAnotherTenantHoldsIsRefusedAsAnUnknownOne is rule 2 seen from the
// next tenant. A person globex has, with globex's real password, comes to acme's
// door: acme has nobody at that address, so the answer is the one an unknown
// address gets — the same status, the same body, no cookie — and it carries the
// same next step. A body that differed would tell a stranger which addresses
// another tenant holds, and a cookie would be a session in a tenant the person
// does not belong to.
func TestAnAddressAnotherTenantHoldsIsRefusedAsAnUnknownOne(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	seed(t, conn, globex)
	personAt(t, conn, globex, "grace@globex.localhost")
	person(t, conn, "ada@acme.localhost")

	signIn := func(email string) (int, string, string) {
		res := call(t, router, http.MethodPost, "/api/v1/auth/login",
			`{"email":"`+email+`","password":"`+authtest.Password+`"}`)
		return res.Code, withoutInstance(res.Body.String()), sessionCookie(res)
	}
	// The door answers acme's own person, so a refusal below is the door's
	// verdict about the address, not a door that answers nobody.
	if code, body, cookie := signIn("ada@acme.localhost"); code != http.StatusOK || cookie == "" {
		t.Fatalf("acme's own person at acme's door = %d (cookie %q) %s, want 200 with a session", code, cookie, body)
	}

	unknownCode, unknown, unknownCookie := signIn("nobody@acme.localhost")
	foreignCode, foreign, foreignCookie := signIn("grace@globex.localhost")
	if unknownCode != http.StatusUnauthorized || foreignCode != http.StatusUnauthorized {
		t.Fatalf("unknown = %d, globex's person at acme = %d; want 401 for both:\n  %s\n  %s",
			unknownCode, foreignCode, unknown, foreign)
	}
	if foreignCookie != "" || unknownCookie != "" {
		t.Errorf("a refused sign-in set a session cookie (unknown %q, globex's person %q)", unknownCookie, foreignCookie)
	}
	if foreign != unknown {
		t.Errorf("globex's person is answered differently from nobody, which tells acme's door who globex has:\n  unknown: %s\n  globex:  %s",
			unknown, foreign)
	}
	if !strings.Contains(foreign, "forgotten-password link under this form") {
		t.Errorf("the refusal names no next step: %s", foreign)
	}
}
