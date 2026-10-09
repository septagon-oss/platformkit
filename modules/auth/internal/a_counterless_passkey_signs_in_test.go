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

func TestAPasskeyWithoutACounterSignsInRepeatedly(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	session := signIn(t, router, "ada@acme.localhost")
	key := newSoftAuthenticator(t, host)
	if code, body := enrolPasskey(t, router, session, key); code != http.StatusCreated {
		t.Fatalf("enrol a counterless passkey = %d %s, want 201", code, body)
	}
	setPasskeySignIn(t, conn, true)

	for attempt := 1; attempt <= 2; attempt++ {
		begin := call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/begin", "")
		if begin.Code != http.StatusOK {
			t.Fatalf("begin passkey sign-in %d = %d %s", attempt, begin.Code, begin.Body.String())
		}
		ceremony, challenge := challengeOf(t, begin.Body.Bytes())
		body, _ := json.Marshal(map[string]any{"ceremony": ceremony, "response": key.asserted(t, challenge)})
		answer := call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/verify", string(body))
		if answer.Code != http.StatusOK || sessionCookie(answer) == "" {
			t.Fatalf("counterless passkey sign-in %d = %d %s; want 200 and a session", attempt, answer.Code, answer.Body.String())
		}
	}

	var warned bool
	var suspectEvents int64
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := tx.DB().Table("passkey_credentials").Select("clone_warning").Row().Scan(&warned); err != nil {
			return err
		}
		return tx.DB().Table("platformkit_outbox").Where("name = ?", contracts.EventFactorSuspect).Count(&suspectEvents).Error
	})
	if err != nil {
		t.Fatalf("read the passkey and its trail: %v", err)
	}
	if warned || suspectEvents != 0 {
		t.Errorf("two valid answers reporting counter zero left clone_warning=%t and %d suspect events; want false and zero", warned, suspectEvents)
	}
}
