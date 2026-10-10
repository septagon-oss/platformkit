package internal_test

// The second use the brief names: a person who holds a passkey gives their
// password, is held at the door, and the passkey is the second factor that lets
// them through — and only after the password, never instead of it.

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
)

func TestAPasskeyIsTheSecondFactorAfterAPassword(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	const email = "ada@acme.localhost"
	person(t, conn, email, contracts.RoleAdmin)
	session := signIn(t, router, email)

	key := newSoftAuthenticator(t, host)
	if code, body := enrolPasskey(t, router, session, key); code != http.StatusCreated {
		t.Fatalf("enrol a passkey = %d %s, want 201", code, body)
	}

	answer := func() answered {
		res := call(t, router, http.MethodPost, "/api/v1/auth/challenge/passkey/begin", `{"email":"`+email+`"}`)
		if res.Code != http.StatusOK {
			t.Fatalf("begin the passkey second factor = %d %s", res.Code, res.Body.String())
		}
		ceremony, challenge := challengeOf(t, res.Body.Bytes())
		body, _ := json.Marshal(map[string]any{"ceremony": ceremony, "response": key.asserted(t, challenge)})
		res = call(t, router, http.MethodPost, "/api/v1/auth/challenge/passkey/verify", string(body))
		return answered{res.Code, res.Body.String(), sessionCookie(res)}
	}

	// Without the password first, a right signature is not a sign-in.
	if early := answer(); early.code == http.StatusOK || early.cookie != "" {
		t.Errorf("the passkey second factor with no password before it = %d %s, want a refusal and no session", early.code, early.body)
	}

	halted := call(t, router, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+email+`","password":"`+authtest.Password+`"}`)
	if halted.Code != http.StatusUnauthorized || sessionCookie(halted) != "" {
		t.Fatalf("the password alone for a person holding a passkey = %d %s, want 401 and no session", halted.Code, halted.Body.String())
	}

	if done := answer(); done.code != http.StatusOK || done.cookie == "" {
		t.Errorf("the passkey second factor after the password = %d %s (cookie %q), want 200 and a session", done.code, done.body, done.cookie)
	}
}

// answered is what one passkey second-factor answer came back with.
type answered struct {
	code   int
	body   string
	cookie string
}
