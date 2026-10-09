package internal_test

// The clone rule, as modules/auth's commit and contracts promise it: a passkey
// whose signature counter goes backwards is refused, is put out of use from then
// on (clone_warning), and the trail holds auth.factor_suspect. The refusal is a
// 401, and the request transaction does not commit a 401 — so the flag and the
// event have to survive the refusal that discovered them.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func TestAPasskeyWhoseCounterWentBackIsRecordedAsSuspect(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	session := signIn(t, router, "ada@acme.localhost")

	key := newSoftAuthenticator(t, host)
	key.counter = 10
	if code, body := enrolPasskey(t, router, session, key); code != http.StatusCreated {
		t.Fatalf("enrol a passkey = %d %s, want 201", code, body)
	}
	setPasskeySignIn(t, conn, true)

	res := call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/begin", "")
	if res.Code != http.StatusOK {
		t.Fatalf("begin a passkey sign-in = %d %s", res.Code, res.Body.String())
	}
	ceremony, challenge := challengeOf(t, res.Body.Bytes())
	key.counter = 5 // a copy of the key that signed fewer times than the original
	body, _ := json.Marshal(map[string]any{"ceremony": ceremony, "response": key.asserted(t, challenge)})
	res = call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/verify", string(body))
	if res.Code != http.StatusUnauthorized || sessionCookie(res) != "" {
		t.Fatalf("a passkey whose counter went from 10 to 5 = %d %s, want 401 and no session", res.Code, res.Body.String())
	}

	var flagged bool
	var trail string
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := tx.DB().Table("passkey_credentials").Select("clone_warning").Row().Scan(&flagged); err != nil {
			return err
		}
		trail = payloads(t, tx)
		var names []string
		if err := tx.DB().Table("platformkit_outbox").Pluck("name", &names).Error; err != nil {
			return err
		}
		trail += "\n" + strings.Join(names, "\n")
		return nil
	})
	if err != nil {
		t.Fatalf("read acme's passkey and trail: %v", err)
	}
	if !flagged {
		t.Errorf("the passkey whose counter went backwards is not marked clone_warning: it is still usable")
	}
	if !strings.Contains(trail, contracts.EventFactorSuspect) {
		t.Errorf("the trail holds no %s after a counter went backwards:\n%s", contracts.EventFactorSuspect, trail)
	}
}
