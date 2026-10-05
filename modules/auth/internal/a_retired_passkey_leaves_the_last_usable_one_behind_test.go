package internal_test

// The other half of rule 8 as contracts.Factors states it: the refusal is about
// the factors that can still answer, so a credential this module retired as
// suspect — a row that answers no prompt, for good — is withdrawable however
// little else the tables hold. Counting it as a factor would refuse its removal
// and offer the person no way to put a dead key out of the list.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func TestARetiredPasskeyIsWithdrawnWhileAUsableOneStays(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	session := signIn(t, router, "ada@acme.localhost")
	suspect := newSoftAuthenticator(t, host)
	suspect.counter = 10
	if code, body := enrolPasskey(t, router, session, suspect); code != http.StatusCreated {
		t.Fatalf("enrol first = %d %s", code, body)
	}
	if code, body := enrolPasskey(t, router, session, newSoftAuthenticator(t, host)); code != http.StatusCreated {
		t.Fatalf("enrol second = %d %s", code, body)
	}
	setDoor(t, router, session, true)
	begun := call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/begin", "")
	ceremony, challenge := challengeOf(t, begun.Body.Bytes())
	suspect.counter = 5
	answer, _ := json.Marshal(map[string]any{"ceremony": ceremony, "response": suspect.asserted(t, challenge)})
	if refused := call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/verify", string(answer)); refused.Code != http.StatusUnauthorized {
		t.Fatalf("backwards counter = %d, want 401", refused.Code)
	}
	retired := retiredPasskey(t, conn)
	keysBefore, eventsBefore := passkeyLedger(t, conn)
	res := call(t, router, http.MethodDelete, "/api/v1/auth/factors/"+retired.String(), "", withSession(session))
	if res.Code >= 400 {
		t.Fatalf("withdrawing a retired passkey = %d %s, want it removed", res.Code, res.Body.String())
	}
	keysAfter, eventsAfter := passkeyLedger(t, conn)
	if keysAfter != keysBefore-1 {
		t.Errorf("the retired passkey left %d credentials, want %d", keysAfter, keysBefore-1)
	}
	if eventsAfter != eventsBefore+1 {
		t.Errorf("the withdrawal published %d withdrawal events, want 1", eventsAfter-eventsBefore)
	}
}

// retiredPasskey is the credential the clone verdict put out of use.
func retiredPasskey(t *testing.T, conn *db.Conn) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Table("passkey_credentials").Where("clone_warning = true").
			Select("id").Row().Scan(&id)
	})
	if err != nil {
		t.Fatalf("read the retired passkey: %v", err)
	}
	return id
}
