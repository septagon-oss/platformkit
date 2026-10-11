package internal_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func TestDisablingPasskeySignInRefusesAnOutstandingCeremony(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	ada := person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	admin := signIn(t, router, "ada@acme.localhost")
	key := newSoftAuthenticator(t, host)
	key.counter = 1
	if code, body := enrolPasskey(t, router, admin, key); code != http.StatusCreated {
		t.Fatalf("enrol = %d %s", code, body)
	}
	setDoor(t, router, admin, true)
	begun := call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/begin", "")
	if begun.Code != http.StatusOK {
		t.Fatalf("begin = %d %s", begun.Code, begun.Body.String())
	}
	ceremony, challenge := challengeOf(t, begun.Body.Bytes())
	setDoor(t, router, admin, false)
	live, _, before := proofLedger(t, conn, ada)
	key.counter = 2
	body, err := json.Marshal(map[string]any{"ceremony": ceremony, "response": key.asserted(t, challenge)})
	if err != nil {
		t.Fatal(err)
	}
	res := call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/verify", string(body))
	if res.Code != http.StatusForbidden {
		t.Errorf("finish after administrator disabled sign-in = %d; want 403", res.Code)
	}
	if sessionCookie(res) != "" {
		t.Error("disabled sign-in issued a session cookie")
	}
	afterLive, _, after := proofLedger(t, conn, ada)
	if afterLive != live {
		t.Errorf("sessions after refusal = %d, before = %d", afterLive, live)
	}
	for _, event := range []string{contracts.EventLoggedIn, contracts.EventFactorUsed} {
		if occurrences(after, event) != occurrences(before, event) {
			t.Errorf("disabled sign-in emitted %s", event)
		}
	}
}
