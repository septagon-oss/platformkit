package internal_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// Both verify routes mount one handler and the ceremony's row names the door, so a
// usernameless ceremony posted to the second-factor route is still a usernameless
// ceremony: a shut door refuses it there too, and the refusal spends it, so opening
// the door again does not bring the same answer back to life.
func TestAShutUsernamelessDoorRefusesItsCeremonyAtEitherVerifyRoute(t *testing.T) {
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
	res := call(t, router, http.MethodPost, "/api/v1/auth/challenge/passkey/verify", string(body))
	if res.Code != http.StatusForbidden {
		t.Errorf("usernameless ceremony at the second-factor route after the door shut = %d; want 403", res.Code)
	}
	if sessionCookie(res) != "" {
		t.Error("a shut door issued a session cookie through the second-factor route")
	}
	afterLive, _, after := proofLedger(t, conn, ada)
	if afterLive != live {
		t.Errorf("sessions after refusal = %d, before = %d", afterLive, live)
	}
	for _, event := range []string{contracts.EventLoggedIn, contracts.EventFactorUsed} {
		if occurrences(after, event) != occurrences(before, event) {
			t.Errorf("a shut door emitted %s", event)
		}
	}

	setDoor(t, router, admin, true)
	again := call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/verify", string(body))
	if again.Code != http.StatusUnauthorized {
		t.Errorf("the refused ceremony answered again after the door reopened = %d; want 401", again.Code)
	}
	if sessionCookie(again) != "" {
		t.Error("a ceremony refused at a shut door opened a session once the door reopened")
	}
}
