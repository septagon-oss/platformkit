package internal_test

// Rule 8 across the two kinds of factor: a person's last factor — here their
// only passkey — is refused removal, the row stays, and no withdrawal is
// published. And two tabs each removing one of two passkeys at once leave one.

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func TestTheLastPasskeyIsNotWithdrawn(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	session := signIn(t, router, "ada@acme.localhost")
	code, body := enrolPasskey(t, router, session, newSoftAuthenticator(t, host))
	if code != http.StatusCreated {
		t.Fatalf("enrol a passkey = %d %s, want 201", code, body)
	}
	var factor contracts.Factor
	_ = json.Unmarshal([]byte(body), &factor)

	res := call(t, router, http.MethodDelete, "/api/v1/auth/factors/"+factor.ID.String(), "", withSession(session))
	if res.Code < 400 {
		t.Fatalf("withdrawing the only passkey = %d %s, want a refusal", res.Code, res.Body.String())
	}
	if keys, withdrawn := passkeyLedger(t, conn); keys != 1 || withdrawn != 0 {
		t.Errorf("after the refused withdrawal: %d passkeys and %d withdrawal events, want 1 and 0", keys, withdrawn)
	}
}

func TestTwoTabsWithdrawingTwoPasskeysLeaveOne(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	session := signIn(t, router, "ada@acme.localhost")
	var ids []string
	for range 2 {
		code, body := enrolPasskey(t, router, session, newSoftAuthenticator(t, host))
		if code != http.StatusCreated {
			t.Fatalf("enrol a passkey = %d %s, want 201", code, body)
		}
		var factor contracts.Factor
		_ = json.Unmarshal([]byte(body), &factor)
		ids = append(ids, factor.ID.String())
	}

	codes := make([]int, 2)
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Go(func() {
			codes[i] = call(t, router, http.MethodDelete, "/api/v1/auth/factors/"+id, "", withSession(session)).Code
		})
	}
	wg.Wait()
	ok := 0
	for _, c := range codes {
		if c < 400 {
			ok++
		}
	}
	if ok != 1 {
		t.Errorf("two tabs withdrawing two passkeys answered %v, want exactly one success", codes)
	}
	if keys, withdrawn := passkeyLedger(t, conn); keys != 1 || withdrawn != 1 {
		t.Errorf("after two concurrent withdrawals: %d passkeys and %d withdrawal events, want 1 and 1", keys, withdrawn)
	}
}

// passkeyLedger is how many passkeys acme holds and how many withdrawals it published.
func passkeyLedger(t *testing.T, conn *db.Conn) (keys, withdrawn int64) {
	t.Helper()
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := tx.DB().Table("passkey_credentials").Count(&keys).Error; err != nil {
			return err
		}
		return tx.DB().Table("platformkit_outbox").Where("name = ?", contracts.EventFactorWithdrawn).Count(&withdrawn).Error
	})
	if err != nil {
		t.Fatalf("read acme's passkey ledger: %v", err)
	}
	return keys, withdrawn
}
