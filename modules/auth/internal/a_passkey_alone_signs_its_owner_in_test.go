package internal_test

// The usernameless door's promise, end to end through the mounted routes: a
// person enrols a passkey, the tenant turns passkey sign-in on, and the passkey
// alone — no address, no password — opens a session. The answered ceremony is
// spent, so the same signed body POSTed a second time opens nothing.

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func TestAnEnrolledPasskeyAloneSignsItsOwnerIn(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	session := signIn(t, router, "ada@acme.localhost")

	key := newSoftAuthenticator(t, host)
	key.counter = 1
	if code, body := enrolPasskey(t, router, session, key); code != http.StatusCreated {
		t.Fatalf("enrol a passkey = %d %s, want 201", code, body)
	}
	setPasskeySignIn(t, conn, true)

	res := call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/begin", "")
	if res.Code != http.StatusOK {
		t.Fatalf("begin a passkey sign-in = %d %s", res.Code, res.Body.String())
	}
	ceremony, challenge := challengeOf(t, res.Body.Bytes())
	key.counter = 2
	body, _ := json.Marshal(map[string]any{"ceremony": ceremony, "response": key.asserted(t, challenge)})

	res = call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/verify", string(body))
	if res.Code != http.StatusOK || sessionCookie(res) == "" {
		t.Fatalf("a valid signature by an enrolled passkey over this tenant's challenge = %d %s (cookie %q), want 200 and a session",
			res.Code, res.Body.String(), sessionCookie(res))
	}
	if n, _ := ceremonies(t, conn); n != 0 {
		t.Errorf("the answered ceremony left %d rows, want 0: an answered nonce is spent", n)
	}

	replay := call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/verify", string(body))
	if replay.Code != http.StatusUnauthorized || sessionCookie(replay) != "" {
		t.Errorf("the same signed body a second time = %d %s, want 401 and no session", replay.Code, replay.Body.String())
	}
}
