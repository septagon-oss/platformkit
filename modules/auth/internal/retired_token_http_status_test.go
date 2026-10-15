package internal_test

import (
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func TestARetiredTokenRepeatingSelfRevocationIsUnauthorized(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	session := signIn(t, router, "ada@acme.localhost")
	key, _ := mintKey(t, router, session, "Phone")
	first := call(t, router, http.MethodPost, revokeSelf, "", bearer(key))
	if first.Code != http.StatusNoContent {
		t.Fatalf("first self-revocation = %d %s, want 204", first.Code, first.Body.String())
	}
	again := call(t, router, http.MethodPost, revokeSelf, "", bearer(key))
	if again.Code != http.StatusUnauthorized {
		t.Errorf("retired token repeating self-revocation = %d %s, want 401", again.Code, again.Body.String())
	}
}
