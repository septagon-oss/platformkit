package internal_test

// Rule 8 from the side a person stands on: contracts.ErrLastFactor refuses the
// withdrawal that takes the last way in away, and a credential retired as suspect
// answers no prompt, for good — counting it as a way in would both refuse taking away the
// last factor that could still answer, because a dead row sits beside it, and leave
// nobody a way to put a dead key out of the list. A guarantee that reaches its caller as
// a 500 is one no screen can act on and an operator reads as an outage.

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func TestARetiredPasskeyIsWithdrawnAsTheRowThatItIs(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	session := signIn(t, router, "ada@acme.localhost")
	retired := newSoftAuthenticator(t, host)
	retired.counter = 10
	code, body := enrolPasskey(t, router, session, retired)
	if code != http.StatusCreated {
		t.Fatalf("enrol = %d %s", code, body)
	}
	var only contracts.Factor
	if err := json.Unmarshal([]byte(body), &only); err != nil {
		t.Fatal(err)
	}
	keys, withdrawn := passkeyLedger(t, conn)
	refused := call(t, router, http.MethodDelete, "/api/v1/auth/factors/"+only.ID.String(), "", withSession(session))
	if refused.Code != http.StatusConflict {
		t.Errorf("withdrawing the only factor = %d %s, want 409", refused.Code, refused.Body.String())
	}
	if keysNow, events := passkeyLedger(t, conn); keysNow != keys || events != withdrawn {
		t.Errorf("the refused withdrawal left %d credentials and %d withdrawals, want %d and %d", keysNow, events, keys, withdrawn)
	}
	setDoor(t, router, session, true) // the one factor this account holds now answers nothing
	begun := call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/begin", "")
	ceremony, challenge := challengeOf(t, begun.Body.Bytes())
	retired.counter = 5 // a copy that signed fewer times than the original
	answer, _ := json.Marshal(map[string]any{"ceremony": ceremony, "response": retired.asserted(t, challenge)})
	if off := call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/verify", string(answer)); off.Code != http.StatusUnauthorized {
		t.Fatalf("a retired passkey answering = %d, want 401", off.Code)
	}
	res := call(t, router, http.MethodDelete, "/api/v1/auth/factors/"+only.ID.String(), "", withSession(session))
	if res.Code >= 400 {
		t.Fatalf("withdrawing the passkey that was retired = %d %s, want it removed", res.Code, res.Body.String())
	}
	if keysNow, events := passkeyLedger(t, conn); keysNow != keys-1 || events != withdrawn+1 {
		t.Errorf("after the retired passkey went: %d credentials and %d withdrawals, want %d and %d", keysNow, events, keys-1, withdrawn+1)
	}
}
