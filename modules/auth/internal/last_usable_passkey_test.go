package internal_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func TestASuspectPasskeyDoesNotPermitWithdrawingTheLastUsableFactor(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	session := signIn(t, router, "ada@acme.localhost")
	suspect := newSoftAuthenticator(t, host)
	suspect.counter = 10
	if code, body := enrolPasskey(t, router, session, suspect); code != http.StatusCreated {
		t.Fatalf("enrol first = %d %s", code, body)
	}
	usable := newSoftAuthenticator(t, host)
	code, body := enrolPasskey(t, router, session, usable)
	if code != http.StatusCreated {
		t.Fatalf("enrol second = %d %s", code, body)
	}
	var factor contracts.Factor
	if err := json.Unmarshal([]byte(body), &factor); err != nil {
		t.Fatal(err)
	}
	setDoor(t, router, session, true)
	begun := call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/begin", "")
	if begun.Code != http.StatusOK {
		t.Fatalf("begin = %d", begun.Code)
	}
	ceremony, challenge := challengeOf(t, begun.Body.Bytes())
	suspect.counter = 5
	answer, err := json.Marshal(map[string]any{"ceremony": ceremony, "response": suspect.asserted(t, challenge)})
	if err != nil {
		t.Fatal(err)
	}
	refused := call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/verify", string(answer))
	if refused.Code != http.StatusUnauthorized {
		t.Fatalf("backwards counter = %d, want 401", refused.Code)
	}
	var flagged int64
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Table("passkey_credentials").Where("clone_warning = true").Count(&flagged).Error
	}); err != nil {
		t.Fatal(err)
	}
	if flagged != 1 {
		t.Fatalf("retired credentials = %d, want 1 before withdrawing the usable factor", flagged)
	}
	keysBefore, eventsBefore := passkeyLedger(t, conn)
	res := call(t, router, http.MethodDelete, "/api/v1/auth/factors/"+factor.ID.String(), "", withSession(session))
	if res.Code < 400 {
		t.Errorf("withdrawing the only usable factor = %d; want refusal", res.Code)
	}
	keysAfter, eventsAfter := passkeyLedger(t, conn)
	if keysAfter != keysBefore {
		t.Errorf("refusal retained %d credentials, want %d", keysAfter, keysBefore)
	}
	if eventsAfter != eventsBefore {
		t.Errorf("refusal emitted %d withdrawals", eventsAfter-eventsBefore)
	}
}
