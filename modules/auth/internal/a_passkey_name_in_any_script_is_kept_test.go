package internal_test

// The finish leg accepts a name of up to 40 characters (its schema says so) and
// the table holds up to 40 characters. A name typed in a script whose characters
// are wider than one byte — Japanese, Greek, an emoji — is still a name of fewer
// than 40 characters, and enrols like any other.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func TestAPasskeyNamedInAWideScriptEnrols(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	session := signIn(t, router, "ada@acme.localhost")

	name := strings.Repeat("鍵", 14) // 14 characters, 42 bytes
	code, body := enrolPasskeyNamed(t, router, session, newSoftAuthenticator(t, host), name)
	if code != http.StatusCreated {
		t.Fatalf("enrol a passkey named %q (14 characters) = %d %s, want 201", name, code, body)
	}
	if !strings.Contains(body, name) {
		t.Errorf("the enrolled factor = %s, want it named %q", body, name)
	}
}
