package internal_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
)

func TestErasureExportsAndRevokesThePersonsPasskeys(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	const email = "ada@acme.localhost"
	id := person(t, conn, email, contracts.RoleAdmin)
	session := signIn(t, router, email)
	key := newSoftAuthenticator(t, host)
	key.counter = 1
	const label = "Ada's laptop"
	if code, body := enrolPasskeyNamed(t, router, session, key, label); code != http.StatusCreated {
		t.Fatalf("enrolment = %d %s", code, body)
	}
	setPasskeySignIn(t, conn, true)
	ctx := tenancy.WithTenant(t.Context(), acme)
	subject := personaldata.Subject{ID: id, Email: email, DisplayName: "Ada"}
	var exported json.RawMessage
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		exported, err = internal.PersonalData().Data.Export(ctx, tx, subject)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(exported), label) {
		t.Errorf("personal-data export omits the enrolled passkey's name %q", label)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return internal.PersonalData().Data.Erase(ctx, tx, subject)
	}); err != nil {
		t.Fatalf("erasure: %v", err)
	}
	var remaining int64
	if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Table("passkey_credentials").Where("user_id = ?", id).Count(&remaining).Error
	}); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Errorf("auth erasure kept %d enrolled passkey credentials", remaining)
	}
	res := call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/begin", "")
	if res.Code != http.StatusOK {
		t.Fatalf("begin a public ceremony = %d %s", res.Code, res.Body.String())
	}
	ceremony, challenge := challengeOf(t, res.Body.Bytes())
	key.counter = 2
	body, err := json.Marshal(map[string]any{"ceremony": ceremony, "response": key.asserted(t, challenge)})
	if err != nil {
		t.Fatal(err)
	}
	res = call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/verify", string(body))
	if res.Code == http.StatusOK || sessionCookie(res) != "" {
		t.Errorf("an erased credential signed in: status=%d sessionIssued=%v", res.Code, sessionCookie(res) != "")
	}
}
