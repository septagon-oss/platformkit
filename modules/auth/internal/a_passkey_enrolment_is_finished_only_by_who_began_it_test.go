package internal_test

// An enrolment ceremony belongs to the person who began it. A colleague who
// holds its id and answers it with their own authenticator enrols nothing — not
// for themselves and not for the person who began it — and the ceremony is still
// there for its owner.

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

func TestAPasskeyEnrolmentIsFinishedOnlyByWhoBeganIt(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	person(t, conn, "bob@acme.localhost")
	ada := signIn(t, router, "ada@acme.localhost")
	bob := signIn(t, router, "bob@acme.localhost")

	res := call(t, router, http.MethodPost, "/api/v1/auth/factors/passkey/begin", "", withSession(ada))
	if res.Code != http.StatusOK {
		t.Fatalf("ada begins an enrolment = %d %s", res.Code, res.Body.String())
	}
	ceremony, challenge := challengeOf(t, res.Body.Bytes())
	key := newSoftAuthenticator(t, host)
	body, _ := json.Marshal(map[string]any{"ceremony": ceremony, "response": key.created(t, challenge), "name": "bob's"})
	res = call(t, router, http.MethodPost, "/api/v1/auth/factors/passkey/finish", string(body), withSession(bob))
	if res.Code < 400 {
		t.Fatalf("bob finishing ada's enrolment = %d %s, want a refusal", res.Code, res.Body.String())
	}
	var keys int64
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Table("passkey_credentials").Count(&keys).Error
	})
	if err != nil {
		t.Fatal(err)
	}
	if keys != 0 {
		t.Errorf("bob's answer to ada's ceremony enrolled %d passkeys, want 0", keys)
	}
	if n, kind := ceremonies(t, conn); n != 1 || kind != "register" {
		t.Errorf("ada's ceremony after bob's refused answer: %d rows of kind %q, want it still there", n, kind)
	}
}
