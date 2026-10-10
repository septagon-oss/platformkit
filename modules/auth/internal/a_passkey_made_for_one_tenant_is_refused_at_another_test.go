package internal_test

// The brief's tenant line: a passkey made for tenant A is refused on tenant B's
// host. The passkey first signs its owner in at A, where it was made — so the
// refusal at B below is about the host, not about a passkey that answers
// nowhere. At B it is presented the strongest way a holder could: signed over
// B's own challenge, for B's relying-party id, from B's origin. Only the
// credential's tenant can refuse it there.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func TestAPasskeyMadeForOneTenantIsRefusedAtAnother(t *testing.T) {
	router, conn := mountTwo(t, mapProviders{}, mapSecrets{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	session := signIn(t, router, "ada@acme.localhost")
	key := newSoftAuthenticator(t, host)
	if code, body := enrolPasskey(t, router, session, key); code != http.StatusCreated {
		t.Fatalf("enrol a passkey at acme = %d %s, want 201", code, body)
	}
	for _, tenant := range []tenancy.Tenant{acme, globex} {
		err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return tx.DB().Exec("INSERT INTO passkey_settings (tenant_id, sign_in) VALUES (?, true)", tenant.ID).Error
		})
		if err != nil {
			t.Fatalf("turn passkey sign-in on for %s: %v", tenant.Slug, err)
		}
	}
	at := func(h string) func(*http.Request) {
		return func(r *http.Request) { r.Host = h; r.URL.Host = h }
	}
	signInAt := func(h string) (int, string, string) {
		res := call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/begin", "", at(h))
		if res.Code != http.StatusOK {
			t.Fatalf("begin a passkey sign-in at %s = %d %s", h, res.Code, res.Body.String())
		}
		ceremony, challenge := challengeOf(t, res.Body.Bytes())
		key.counter++
		body, _ := json.Marshal(map[string]any{"ceremony": ceremony, "response": key.asserted(t, challenge)})
		res = call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/verify", string(body), at(h))
		return res.Code, res.Body.String(), sessionCookie(res)
	}

	if code, body, cookie := signInAt(host); code != http.StatusOK || cookie == "" {
		t.Fatalf("the passkey at the host it was made for = %d %s, want 200 and a session", code, body)
	}

	key.rpID, key.origin = secondHost, "http://"+secondHost
	code, body, cookie := signInAt(secondHost)
	if code != http.StatusUnauthorized || cookie != "" {
		t.Errorf("acme's passkey at globex's host = %d %s (cookie %q), want 401 and no session", code, body, cookie)
	}
	var sessions int64
	err := db.Run(tenancy.WithTenant(t.Context(), globex), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Table("sessions").Count(&sessions).Error
	})
	if err != nil {
		t.Fatal(err)
	}
	if sessions != 0 {
		t.Errorf("globex holds %d sessions after acme's passkey was presented there, want 0", sessions)
	}
}
