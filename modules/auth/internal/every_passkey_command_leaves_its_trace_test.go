package internal_test

// Pillars 3 and 4 for passkeys: an enrolment and a use each leave an event on
// the tenant's outbox — the record modules/audit keeps — and each carries the
// trace of the request that caused it, so the audit line joins the request. The
// request names its own traceparent; the outbox row must quote that trace.

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

func TestEveryPasskeyCommandLeavesItsTrace(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	session := signIn(t, router, "ada@acme.localhost")

	traceOf := map[string]string{}
	traced := func(id string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Header.Set("traceparent", "00-"+id+"-00f067aa0ba902b7-01")
			router.ServeHTTP(w, r)
		})
	}
	const enrolTrace = "4bf92f3577b34da6a3ce929d0e0e4736"
	const useTrace = "5cf92f3577b34da6a3ce929d0e0e4737"

	key := newSoftAuthenticator(t, host)
	key.counter = 1
	if code, body := enrolPasskey(t, traced(enrolTrace), session, key); code != http.StatusCreated {
		t.Fatalf("enrol a passkey = %d %s, want 201", code, body)
	}
	setPasskeySignIn(t, conn, true)

	res := call(t, traced(useTrace), http.MethodPost, "/api/v1/auth/login/passkey/begin", "")
	if res.Code != http.StatusOK {
		t.Fatalf("begin a passkey sign-in = %d %s", res.Code, res.Body.String())
	}
	ceremony, challenge := challengeOf(t, res.Body.Bytes())
	key.counter = 2
	body, _ := json.Marshal(map[string]any{"ceremony": ceremony, "response": key.asserted(t, challenge)})
	res = call(t, traced(useTrace), http.MethodPost, "/api/v1/auth/login/passkey/verify", string(body))
	if res.Code != http.StatusOK {
		t.Fatalf("sign in with the passkey = %d %s, want 200", res.Code, res.Body.String())
	}

	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		rows, err := tx.DB().Table("platformkit_outbox").Select("name, coalesce(traceparent, '')").Rows()
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var name, parent string
			if err := rows.Scan(&name, &parent); err != nil {
				return err
			}
			traceOf[name] += parent + " "
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("read acme's outbox: %v", err)
	}
	for name, want := range map[string]string{
		contracts.EventFactorEnrolled: enrolTrace,
		contracts.EventFactorUsed:     useTrace,
	} {
		got, ok := traceOf[name]
		if !ok {
			t.Errorf("no %s on the tenant's outbox; the audit trail has no record of it", name)
			continue
		}
		if !strings.Contains(got, want) {
			t.Errorf("%s carries traceparent %q, want the trace %s of the request that caused it", name, strings.TrimSpace(got), want)
		}
	}
}
